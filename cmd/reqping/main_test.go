package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/testutil"
)

func TestBinary(t *testing.T) {
	bin := testutil.BuildBinary(t, ".", "reqping")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/gone", http.NotFound)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Run("healthy", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--contains", "ok", srv.URL+"/health")
		if r.Code != 0 || !strings.Contains(r.Stdout, "✔ 200") || !strings.Contains(r.Stdout, "✔ 1 healthy, ✘ 0 unhealthy") {
			t.Fatalf("code=%d stdout=%q stderr=%q", r.Code, r.Stdout, r.Stderr)
		}
		if strings.Contains(r.Stdout, "\x1b[") {
			t.Error("NO_COLOR output contains ANSI codes")
		}
	})

	t.Run("unhealthy exits 1", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, srv.URL+"/health", srv.URL+"/gone")
		if r.Code != 1 || !strings.Contains(r.Stdout, "expected 2xx, got 404") || !strings.Contains(r.Stdout, "✘ 1 unhealthy") {
			t.Fatalf("code=%d stdout=%q stderr=%q", r.Code, r.Stdout, r.Stderr)
		}
	})

	t.Run("json from stdin targets", func(t *testing.T) {
		list := fmt.Sprintf("# targets\nhealth %s/health\ngone %s/gone 404\n", srv.URL, srv.URL)
		r := testutil.RunBinary(t, bin, strings.NewReader(list), "--json", "--count", "2", "-f", "-")
		if r.Code != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", r.Code, r.Stdout, r.Stderr)
		}
		var rep struct {
			Healthy bool `json:"healthy"`
			Targets []struct {
				Name       string `json:"name"`
				StatusCode int    `json:"status_code"`
				Stats      struct {
					Count int `json:"count"`
					OK    int `json:"ok"`
				} `json:"stats"`
			} `json:"targets"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &rep); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, r.Stdout)
		}
		if !rep.Healthy || len(rep.Targets) != 2 || rep.Targets[1].Name != "gone" || rep.Targets[1].StatusCode != http.StatusNotFound ||
			rep.Targets[0].Stats.Count != 2 || rep.Targets[0].Stats.OK != 2 {
			t.Fatalf("report %+v", rep)
		}
	})

	t.Run("usage error exits 2", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "example.com")
		if r.Code != 2 || !strings.Contains(r.Stderr, "reqping: error: invalid URL \"example.com\": missing scheme") {
			t.Fatalf("code=%d stderr=%q", r.Code, r.Stderr)
		}
	})

	t.Run("version", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--version")
		if r.Code != 0 || !strings.HasPrefix(r.Stdout, "reqping ") {
			t.Fatalf("code=%d stdout=%q", r.Code, r.Stdout)
		}
	})
}
