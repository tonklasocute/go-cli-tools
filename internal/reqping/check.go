package reqping

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

const (
	// MaxBodyBytes caps how much of a response body is read for --contains.
	MaxBodyBytes = 1 << 20
	// CertWarnDays is the threshold under which a certificate's remaining
	// lifetime is flagged as a warning (the target stays healthy).
	CertWarnDays = 14
	// drainBytes is how much of an unchecked body is drained so the
	// connection can be reused by the next request.
	drainBytes = 64 << 10
)

// Checker performs HTTP health checks. The zero value is usable: GET,
// redirects followed, 10s timeout, one request per target, 10 targets at a
// time, http.DefaultTransport and the real clock.
type Checker struct {
	// Transport sends the requests; nil means http.DefaultTransport.
	Transport http.RoundTripper
	// Method is GET (default) or HEAD.
	Method string
	// Header is added to every request.
	Header http.Header
	// UserAgent is sent unless Header sets User-Agent itself.
	UserAgent string
	// Timeout bounds each individual request, body read included.
	Timeout time.Duration
	// Contains, when non-empty, must appear in the first MaxBodyBytes of
	// the body for the target to be healthy.
	Contains string
	// NoFollow disables following redirects.
	NoFollow bool
	// Count is the number of requests per target (default 1).
	Count int
	// Concurrency is how many targets are checked at once (default 10).
	Concurrency int
	// Now is the clock used for certificate expiry; nil means time.Now.
	Now func() time.Time
}

// Result is the outcome of checking one target.
type Result struct {
	Target Target
	Method string
	// Healthy is true when every request succeeded and matched.
	Healthy bool
	// StatusCode is the status of the last response (0 if none).
	StatusCode int
	// Problem explains why the target is unhealthy (empty when healthy).
	Problem string
	// Warning flags a healthy target that needs attention soon.
	Warning string
	// RedirectedTo is the final URL after redirects, or the Location of an
	// unfollowed redirect.
	RedirectedTo string
	// TLSExpires is the leaf certificate's NotAfter (zero for plain http).
	TLSExpires time.Time
	// TLSDays is the number of whole days until TLSExpires (negative when
	// expired); only meaningful when TLSExpires is set.
	TLSDays int
	// TLSExpired is true when the certificate had already expired.
	TLSExpired bool
	// OK is the number of successful requests out of Count.
	OK int
	// Count is the number of requests made.
	Count int
	// Latency summarises the requests that received a response.
	Latency Latency
}

// HasTLS reports whether certificate information was collected.
func (r *Result) HasTLS() bool { return !r.TLSExpires.IsZero() }

// Report is the outcome of one round of checks.
type Report struct {
	Results   []Result
	CheckedAt time.Time
	Elapsed   time.Duration
}

// Healthy reports whether every target is healthy.
func (r *Report) Healthy() bool {
	for i := range r.Results {
		if !r.Results[i].Healthy {
			return false
		}
	}
	return true
}

// Counts returns the number of healthy, unhealthy and warned targets.
func (r *Report) Counts() (healthy, unhealthy, warned int) {
	for i := range r.Results {
		res := &r.Results[i]
		if res.Healthy {
			healthy++
		} else {
			unhealthy++
		}
		if res.Warning != "" {
			warned++
		}
	}
	return healthy, unhealthy, warned
}

// NewTransport returns a transport cloned from http.DefaultTransport with
// the given TLS settings. roots may be nil to use the system pool.
func NewTransport(insecure bool, roots *x509.CertPool) *http.Transport {
	var tr *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	tr.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		// Only set by the explicit --insecure flag, which also warns on stderr.
		InsecureSkipVerify: insecure, //nolint:gosec // explicit user opt-in via --insecure
	}
	return tr
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) method() string {
	if c.Method == "" {
		return http.MethodGet
	}
	return c.Method
}

func (c *Checker) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 10 * time.Second
	}
	return c.Timeout
}

func (c *Checker) client() *http.Client {
	cl := &http.Client{Transport: c.Transport}
	if c.NoFollow {
		cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return cl
}

// CheckAll checks every target concurrently and returns the results in input
// order. When ctx is cancelled, outstanding checks stop promptly; callers
// should test ctx.Err() before using the report.
func (c *Checker) CheckAll(ctx context.Context, targets []Target) Report {
	start := time.Now()
	rep := Report{Results: make([]Result, len(targets)), CheckedAt: c.now()}
	conc := c.Concurrency
	if conc <= 0 {
		conc = 10
	}
	cl := c.client()
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
loop:
	for i := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rep.Results[i] = c.check(ctx, cl, &targets[i])
		}()
	}
	wg.Wait()
	rep.Elapsed = time.Since(start)
	return rep
}

// Check checks a single target, performing Count requests sequentially.
func (c *Checker) Check(ctx context.Context, t Target) Result {
	return c.check(ctx, c.client(), &t)
}

func (c *Checker) check(ctx context.Context, cl *http.Client, t *Target) Result {
	count := max(c.Count, 1)
	res := Result{Target: *t, Method: c.method()}
	var samples []time.Duration
	for i := 0; i < count && ctx.Err() == nil; i++ {
		a := c.attempt(ctx, cl, t)
		res.Count++
		if a.gotResponse {
			samples = append(samples, a.latency)
		}
		if a.problem == "" {
			res.OK++
		} else {
			res.Problem = a.problem
		}
		res.StatusCode = a.status
		res.RedirectedTo = a.redirectedTo
		if !a.notAfter.IsZero() {
			res.TLSExpires = a.notAfter
		}
	}
	res.Latency = Summarize(samples)
	res.Healthy = res.Count > 0 && res.OK == res.Count
	if res.HasTLS() {
		left := res.TLSExpires.Sub(c.now())
		res.TLSDays = int(left / (24 * time.Hour))
		res.TLSExpired = left <= 0
		switch {
		case res.TLSExpired || res.TLSDays >= CertWarnDays:
		case res.TLSDays == 0:
			res.Warning = "certificate expires in less than a day"
		default:
			res.Warning = "certificate expires in " + ui.Plural(res.TLSDays, "day", "days")
		}
	}
	return res
}

// attempt is the outcome of one HTTP request.
type attempt struct {
	gotResponse  bool
	status       int
	latency      time.Duration
	problem      string
	redirectedTo string
	notAfter     time.Time
}

func (c *Checker) attempt(ctx context.Context, cl *http.Client, t *Target) attempt {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, c.method(), t.URL, http.NoBody)
	if err != nil {
		return attempt{problem: err.Error()}
	}
	for k, vs := range c.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if h := req.Header.Get("Host"); h != "" {
		req.Host = h
	}
	if req.Header.Get("User-Agent") == "" && c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	start := time.Now()
	resp, err := cl.Do(req)
	lat := time.Since(start)
	if err != nil {
		return attempt{latency: lat, problem: describeError(ctx, err, c.timeout())}
	}
	defer resp.Body.Close()

	a := attempt{gotResponse: true, status: resp.StatusCode, latency: lat}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		a.notAfter = resp.TLS.PeerCertificates[0].NotAfter
	}
	if c.NoFollow {
		if loc, err := resp.Location(); err == nil && resp.StatusCode/100 == 3 {
			a.redirectedTo = loc.String()
		}
	} else if final := resp.Request.URL.String(); final != t.URL {
		a.redirectedTo = final
	}

	switch {
	case !t.Expect.Match(resp.StatusCode):
		a.problem = fmt.Sprintf("expected %s, got %d", t.Expect, resp.StatusCode)
	case c.Contains != "":
		body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
		if err != nil {
			a.problem = "reading body: " + describeError(ctx, err, c.timeout())
		} else if !bytes.Contains(body, []byte(c.Contains)) {
			a.problem = fmt.Sprintf("body missing %q", c.Contains)
		}
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
	}
	if a.problem == "" && !a.notAfter.IsZero() && c.now().After(a.notAfter) {
		a.problem = "certificate expired on " + a.notAfter.UTC().Format(time.DateOnly)
	}
	return a
}

// describeError turns a transport error into a short, human-readable note.
func describeError(ctx context.Context, err error, timeout time.Duration) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() ||
		errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout after " + timeout.String()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns: " + dnsErr.Err + " (" + dnsErr.Name + ")"
	}
	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(strings.ToLower(err.Error()), "refused") {
		return "connection refused"
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return "tls: " + certErr.Err.Error()
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}
