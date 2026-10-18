package reqping

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func sampleReport() Report {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	lat := Summarize(msd(10, 20, 30))
	return Report{
		CheckedAt: time.Date(2026, 10, 18, 13, 4, 5, 0, time.UTC),
		Elapsed:   1234 * time.Millisecond,
		Results: []Result{
			{
				Target: Target{Name: "api", URL: "https://api.example.com", Expect: DefaultExpect(true)},
				Method: "GET", Healthy: true, StatusCode: 200, TLSExpires: exp, TLSDays: 87,
				OK: 3, Count: 3, Latency: lat,
			},
			{
				Target: Target{Name: "soon", URL: "https://soon.example.com", Expect: mustExpect("200")},
				Method: "GET", Healthy: true, StatusCode: 200, TLSExpires: exp, TLSDays: 5,
				Warning: "certificate expires in 5 days", OK: 3, Count: 3, Latency: lat,
				RedirectedTo: "https://soon.example.com/home",
			},
			{
				Target: Target{Name: "down", URL: "http://down.example.com", Expect: DefaultExpect(true)},
				Method: "GET", Problem: "connection refused", OK: 0, Count: 3,
			},
			{
				Target: Target{Name: "old", URL: "https://old.example.com", Expect: DefaultExpect(true)},
				Method: "GET", StatusCode: 200, TLSExpires: exp, TLSDays: -3, TLSExpired: true,
				Problem: "certificate expired on 2027-01-01", OK: 1, Count: 3, Latency: lat,
			},
		},
	}
}

func TestRenderText(t *testing.T) {
	rep := sampleReport()
	var b bytes.Buffer
	if err := RenderText(&b, ui.NewTheme(false), &rep, false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"NAME  STATUS  LATENCY  TLS        NOTE\n",
		"api   ✔ 200      20ms  87d\n",
		"soon  ✔ 200      20ms  ▲ 5d       certificate expires in 5 days · redirected to https://soon.example.com/home\n",
		"down  ✘ ERR         —  —          connection refused\n",
		"old   ✘ 200      20ms  ✘ expired  certificate expired on 2027-01-01\n",
		"✔ 2 healthy, ▲ 1 warning, ✘ 2 unhealthy · 1.23s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTextStats(t *testing.T) {
	rep := sampleReport()
	var b bytes.Buffer
	if err := RenderText(&b, ui.NewTheme(false), &rep, true); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"NAME  STATUS   OK   MIN   AVG   P95   MAX",
		"api   ✔ 200   3/3  10ms  20ms  30ms  30ms",
		"down  ✘ ERR   0/3     —     —     —     —",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTextColorSummary(t *testing.T) {
	rep := Report{Results: []Result{{Target: Target{Name: "a"}, Healthy: true, StatusCode: 200}}}
	th := ui.NewTheme(true)
	s := Summary(th, &rep)
	if !strings.Contains(s, "\x1b[") || !strings.Contains(s, "0 unhealthy") {
		t.Fatalf("summary %q", s)
	}
}

func TestToJSON(t *testing.T) {
	rep := sampleReport()
	b, err := json.Marshal(rep.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["healthy"] != false || got["elapsed_ms"] != 1234.0 || got["checked_at"] != "2026-10-18T13:04:05Z" {
		t.Fatalf("top level %v", got)
	}
	targets := got["targets"].([]any)
	api := targets[0].(map[string]any)
	wantKeys := []string{"name", "url", "method", "healthy", "status_code", "expected", "latency_ms", "error",
		"warning", "tls_expiry_days", "tls_expires_at", "redirected_to", "stats"}
	for _, k := range wantKeys {
		if _, ok := api[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if api["status_code"] != 200.0 || api["latency_ms"] != 20.0 || api["tls_expiry_days"] != 87.0 ||
		api["tls_expires_at"] != "2027-01-01T00:00:00Z" || api["error"] != nil || api["expected"] != "2xx" {
		t.Errorf("api = %v", api)
	}
	stats := api["stats"].(map[string]any)
	if stats["count"] != 3.0 || stats["ok"] != 3.0 || stats["min_ms"] != 10.0 || stats["p95_ms"] != 30.0 || stats["max_ms"] != 30.0 {
		t.Errorf("stats = %v", stats)
	}
	down := targets[2].(map[string]any)
	if down["status_code"] != nil || down["latency_ms"] != nil || down["tls_expiry_days"] != nil || down["error"] != "connection refused" {
		t.Errorf("down = %v", down)
	}
	if targets[1].(map[string]any)["redirected_to"] != "https://soon.example.com/home" {
		t.Errorf("redirected_to = %v", targets[1])
	}
}
