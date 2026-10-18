package reqping

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newServer starts an httptest server that is closed when the test ends.
func newServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func target(t *testing.T, rawURL, expect string) Target {
	t.Helper()
	e := DefaultExpect(true)
	if expect != "" {
		e = mustExpect(expect)
	}
	tg, err := NewTarget("", rawURL, e)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestCheckHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "status: ok") })
	mux.HandleFunc("/fail", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	mux.HandleFunc("/created", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusFound)
	})
	srv := newServer(t, mux.ServeHTTP)

	tests := []struct {
		name        string
		path        string
		expect      string
		checker     Checker
		wantHealthy bool
		wantStatus  int
		wantProblem string
		wantRedir   string
	}{
		{name: "healthy 200", path: "/ok", wantHealthy: true, wantStatus: 200},
		{name: "500", path: "/fail", wantStatus: 500, wantProblem: "expected 2xx, got 500"},
		{name: "wrong status vs expected", path: "/created", expect: "200", wantStatus: 201, wantProblem: "expected 200, got 201"},
		{name: "status list matches", path: "/created", expect: "200,201", wantHealthy: true, wantStatus: 201},
		{name: "contains hit", path: "/ok", checker: Checker{Contains: "ok"}, wantHealthy: true, wantStatus: 200},
		{name: "contains miss", path: "/ok", checker: Checker{Contains: "healthy"}, wantStatus: 200, wantProblem: `body missing "healthy"`},
		{name: "redirect followed", path: "/redirect", wantHealthy: true, wantStatus: 200, wantRedir: srv.URL + "/ok"},
		{
			name: "redirect not followed", path: "/redirect", expect: "2xx,3xx", checker: Checker{NoFollow: true},
			wantHealthy: true, wantStatus: 302, wantRedir: srv.URL + "/ok",
		},
		{
			name: "redirect not followed, 2xx expected", path: "/redirect", expect: "200", checker: Checker{NoFollow: true},
			wantStatus: 302, wantProblem: "expected 200, got 302", wantRedir: srv.URL + "/ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.checker
			res := c.Check(context.Background(), target(t, srv.URL+tt.path, tt.expect))
			if res.Healthy != tt.wantHealthy || res.StatusCode != tt.wantStatus || res.Problem != tt.wantProblem {
				t.Fatalf("got healthy=%v status=%d problem=%q", res.Healthy, res.StatusCode, res.Problem)
			}
			if res.RedirectedTo != tt.wantRedir {
				t.Errorf("RedirectedTo = %q, want %q", res.RedirectedTo, tt.wantRedir)
			}
			if res.HasTLS() {
				t.Error("plain http should have no TLS info")
			}
			if res.Latency.N != 1 || res.Count != 1 || res.Method != http.MethodGet {
				t.Errorf("unexpected result %+v", res)
			}
		})
	}
}

func TestCheckHeadersMethodAndUserAgent(t *testing.T) {
	type seen struct{ method, auth, ua, host string }
	got := make(chan seen, 1)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.Method, r.Header.Get("Authorization"), r.UserAgent(), r.Host}
	})
	c := Checker{
		Method:    http.MethodHead,
		Header:    http.Header{"Authorization": {"Bearer t0k"}, "Host": {"example.test"}},
		UserAgent: "reqping/test",
	}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if !res.Healthy || res.Method != http.MethodHead {
		t.Fatalf("result %+v", res)
	}
	s := <-got
	if s.method != http.MethodHead || s.auth != "Bearer t0k" || s.ua != "reqping/test" || s.host != "example.test" {
		t.Fatalf("server saw %+v", s)
	}

	// A User-Agent header overrides the default one.
	c.Header = http.Header{"User-Agent": {"custom/1"}}
	c.Check(context.Background(), target(t, srv.URL, ""))
	if s := <-got; s.ua != "custom/1" {
		t.Fatalf("User-Agent = %q", s.ua)
	}
}

func TestCheckTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() { close(release) }) // runs before srv.Close (LIFO)
	c := Checker{Timeout: 50 * time.Millisecond}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if res.Healthy || res.StatusCode != 0 || res.Problem != "timeout after 50ms" || res.Latency.N != 0 {
		t.Fatalf("result %+v", res)
	}
}

func TestCheckConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	var c Checker
	res := c.Check(context.Background(), target(t, u, ""))
	if res.Healthy || res.StatusCode != 0 || res.Problem == "" {
		t.Fatalf("result %+v", res)
	}
	if !strings.Contains(res.Problem, "refused") {
		t.Logf("note: problem = %q", res.Problem)
	}
}

func TestCheckCountStats(t *testing.T) {
	var n atomic.Int32
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	c := Checker{Count: 4}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if res.Healthy || res.Count != 4 || res.OK != 3 || res.Latency.N != 4 {
		t.Fatalf("result %+v", res)
	}
	if res.Problem != "expected 2xx, got 503" || res.StatusCode != http.StatusOK {
		t.Errorf("problem=%q status=%d", res.Problem, res.StatusCode)
	}
	if res.Latency.Min > res.Latency.Avg || res.Latency.Avg > res.Latency.Max || res.Latency.P95 > res.Latency.Max {
		t.Errorf("inconsistent latency %+v", res.Latency)
	}
}

func TestCheckAllOrderAndConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	gate := make(chan struct{})
	var openGate sync.Once
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		if cur == 2 {
			openGate.Do(func() { close(gate) }) // both concurrent slots are busy
		}
		<-gate
		inFlight.Add(-1)
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	var targets []Target
	for i := 0; i < 6; i++ {
		p := fmt.Sprintf("/t%d", i)
		if i == 3 {
			p = "/bad"
		}
		tg := target(t, srv.URL+p, "")
		tg.Name = fmt.Sprintf("n%d", i)
		targets = append(targets, tg)
	}
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := Checker{Concurrency: 2, Now: func() time.Time { return fixed }}
	rep := c.CheckAll(context.Background(), targets)
	for i, r := range rep.Results {
		if r.Target.Name != fmt.Sprintf("n%d", i) {
			t.Fatalf("result %d is %q: order not preserved", i, r.Target.Name)
		}
	}
	if peak.Load() > 2 {
		t.Errorf("peak concurrency %d > 2", peak.Load())
	}
	if rep.Healthy() {
		t.Error("report should be unhealthy")
	}
	h, u, w := rep.Counts()
	if h != 5 || u != 1 || w != 0 {
		t.Errorf("counts = %d %d %d", h, u, w)
	}
	if !rep.CheckedAt.Equal(fixed) {
		t.Errorf("CheckedAt = %v", rep.CheckedAt)
	}
}

func TestCheckAllCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var c Checker
	rep := c.CheckAll(ctx, []Target{{Name: "x", URL: "http://127.0.0.1:1", Expect: DefaultExpect(true)}})
	if len(rep.Results) != 1 || rep.Results[0].Count != 0 || rep.Results[0].Healthy {
		t.Fatalf("report %+v", rep)
	}
}

func TestCheckTLSServer(t *testing.T) {
	srv := newQuietTLSServer(t, nil)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := Checker{Transport: srv.Client().Transport, Now: func() time.Time { return now }}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if !res.Healthy || !res.HasTLS() || res.Warning != "" {
		t.Fatalf("result %+v", res)
	}
	notAfter := srv.Certificate().NotAfter
	want := int(notAfter.Sub(now) / (24 * time.Hour))
	if res.TLSDays != want || !res.TLSExpires.Equal(notAfter) {
		t.Fatalf("TLSDays = %d (%v), want %d (%v)", res.TLSDays, res.TLSExpires, want, notAfter)
	}

	// Without trusting the test CA the handshake fails.
	var strict Checker
	res = strict.Check(context.Background(), target(t, srv.URL, ""))
	if res.Healthy || !strings.HasPrefix(res.Problem, "tls: ") {
		t.Fatalf("untrusted result %+v", res)
	}
}

// selfSigned returns a TLS certificate for 127.0.0.1/localhost valid from
// notBefore to notAfter, plus a pool that trusts it.
func selfSigned(t *testing.T, notBefore, notAfter time.Time) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "reqping test"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// newQuietTLSServer starts an empty-200 TLS server (with httptest's own
// certificate when cfg is nil) whose handshake-error logging is silenced.
func newQuietTLSServer(t *testing.T, cfg *tls.Config) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func newTLSServerWithCert(t *testing.T, cert tls.Certificate) *httptest.Server {
	t.Helper()
	return newQuietTLSServer(t, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
}

func TestCheckCertExpiringSoonIsWarning(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cert, pool := selfSigned(t, now.Add(-time.Hour), now.Add(5*24*time.Hour))
	srv := newTLSServerWithCert(t, cert)
	c := Checker{Transport: NewTransport(false, pool), Now: func() time.Time { return now }}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if !res.Healthy || res.TLSDays != 5 || res.TLSExpired {
		t.Fatalf("result %+v", res)
	}
	if res.Warning != "certificate expires in 5 days" {
		t.Errorf("Warning = %q", res.Warning)
	}
	rep := Report{Results: []Result{res}}
	if h, u, w := rep.Counts(); h != 1 || u != 0 || w != 1 {
		t.Errorf("counts = %d %d %d", h, u, w)
	}

	// Less than a day left.
	c.Now = func() time.Time { return now.Add(5*24*time.Hour - time.Hour) }
	if res := c.Check(context.Background(), target(t, srv.URL, "")); res.Warning != "certificate expires in less than a day" {
		t.Errorf("Warning = %q", res.Warning)
	}
}

func TestCheckExpiredCert(t *testing.T) {
	now := time.Now()
	cert, pool := selfSigned(t, now.Add(-10*24*time.Hour), now.Add(-2*24*time.Hour))
	srv := newTLSServerWithCert(t, cert)

	// Verification on: the handshake itself fails.
	c := Checker{Transport: NewTransport(false, pool)}
	res := c.Check(context.Background(), target(t, srv.URL, ""))
	if res.Healthy || !strings.Contains(res.Problem, "expired") {
		t.Fatalf("verified result %+v", res)
	}

	// --insecure: the request succeeds but the target is unhealthy.
	c = Checker{Transport: NewTransport(true, nil)}
	res = c.Check(context.Background(), target(t, srv.URL, ""))
	if res.Healthy || !res.TLSExpired || res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Problem, "certificate expired on ") {
		t.Fatalf("insecure result %+v", res)
	}
	if res.TLSDays > -1 || res.Warning != "" {
		t.Errorf("TLSDays = %d, Warning = %q", res.TLSDays, res.Warning)
	}
}

func TestDescribeError(t *testing.T) {
	bg := context.Background()
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout after 1s"},
		{&net.DNSError{Err: "no such host", Name: "nope.invalid"}, "dns: no such host (nope.invalid)"},
		{errors.New("dial tcp: connection refused"), "connection refused"},
		{errors.New("something odd"), "something odd"},
	}
	for _, tt := range tests {
		if got := describeError(bg, tt.err, time.Second); got != tt.want {
			t.Errorf("describeError(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestCheckBadRequestURL(t *testing.T) {
	var c Checker
	res := c.Check(context.Background(), Target{Name: "x", URL: "http://[::1", Expect: DefaultExpect(true)})
	if res.Healthy || res.Problem == "" {
		t.Fatalf("result %+v", res)
	}
}
