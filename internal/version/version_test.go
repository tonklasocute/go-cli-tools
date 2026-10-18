package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	origRead := readBuildInfo
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() {
		readBuildInfo = origRead
		Version, Commit, Date = origV, origC, origD
	})

	tests := []struct {
		name                string
		version, commit     string
		date                string
		bi                  *debug.BuildInfo
		ok                  bool
		wantV, wantC, wantD string
	}{
		{
			name: "ldflags win", version: "v1.2.3", commit: "abc", date: "2026-01-01",
			bi: &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, ok: true,
			wantV: "v1.2.3", wantC: "abc", wantD: "2026-01-01",
		},
		{
			name: "fallback to build info", version: "dev", commit: "none", date: "unknown",
			bi: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.1.0"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123"},
					{Key: "vcs.time", Value: "2026-10-18T00:00:00Z"},
				},
			}, ok: true,
			wantV: "v0.1.0", wantC: "0123456789ab", wantD: "2026-10-18T00:00:00Z",
		},
		{
			name: "devel build keeps dev", version: "dev", commit: "none", date: "unknown",
			bi: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true,
			wantV: "dev", wantC: "none", wantD: "unknown",
		},
		{
			name: "no build info", version: "dev", commit: "none", date: "unknown",
			ok: false, wantV: "dev", wantC: "none", wantD: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version, Commit, Date = tt.version, tt.commit, tt.date
			readBuildInfo = func() (*debug.BuildInfo, bool) { return tt.bi, tt.ok }
			got := Get()
			if got.Version != tt.wantV || got.Commit != tt.wantC || got.Date != tt.wantD {
				t.Fatalf("Get() = %+v, want %s/%s/%s", got, tt.wantV, tt.wantC, tt.wantD)
			}
			if got.GoVersion == "" || !strings.Contains(got.Platform, "/") {
				t.Fatalf("missing runtime info: %+v", got)
			}
		})
	}
}

func TestString(t *testing.T) {
	s := String("envcheck")
	if !strings.HasPrefix(s, "envcheck ") || !strings.HasSuffix(s, "\n") {
		t.Fatalf("unexpected banner %q", s)
	}
}
