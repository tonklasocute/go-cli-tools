package reqping

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseExpect(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		match   []int
		nomatch []int
		wantErr string
	}{
		{in: "200", want: "200", match: []int{200}, nomatch: []int{201, 404}},
		{in: "2xx", want: "2xx", match: []int{200, 204, 299}, nomatch: []int{199, 300}},
		{in: "2XX", want: "2xx", match: []int{201}},
		{in: "200, 204,3xx", want: "200,204,3xx", match: []int{200, 204, 301}, nomatch: []int{201, 404}},
		{in: "", wantErr: "empty"},
		{in: "abc", wantErr: `invalid status "abc"`},
		{in: "99", wantErr: "invalid status"},
		{in: "600", wantErr: "invalid status"},
		{in: "0200", wantErr: "invalid status"},
		{in: "6xx", wantErr: "invalid status"},
		{in: "200,", wantErr: "invalid status"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			e, err := ParseExpect(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if e.String() != tt.want {
				t.Errorf("String() = %q, want %q", e.String(), tt.want)
			}
			for _, c := range tt.match {
				if !e.Match(c) {
					t.Errorf("Match(%d) = false", c)
				}
			}
			for _, c := range tt.nomatch {
				if e.Match(c) {
					t.Errorf("Match(%d) = true", c)
				}
			}
		})
	}
}

func TestDefaultExpect(t *testing.T) {
	if got := DefaultExpect(true).String(); got != "2xx" {
		t.Errorf("follow default = %q", got)
	}
	if got := DefaultExpect(false).String(); got != "2xx,3xx" {
		t.Errorf("no-follow default = %q", got)
	}
	if !(Expect{}).IsZero() || DefaultExpect(true).IsZero() {
		t.Error("IsZero wrong")
	}
	if !mustExpect("bad").IsZero() {
		t.Error("mustExpect(bad) should be zero")
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		in, wantErr string
	}{
		{in: "https://go.dev"},
		{in: "HTTP://example.com:8080/x?y=1"},
		{in: "go.dev", wantErr: "missing scheme"},
		{in: "example.com:8080", wantErr: "missing scheme"},
		{in: "ftp://example.com", wantErr: `unsupported scheme "ftp"`},
		{in: "https://", wantErr: "missing host"},
		{in: "https://exa mple.com", wantErr: "invalid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := ParseURL(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseTargets(t *testing.T) {
	def := DefaultExpect(true)
	type want struct{ name, url, expect string }
	tests := []struct {
		name    string
		in      string
		want    []want
		wantErr string
	}{
		{
			name: "all forms",
			in: "# header comment\n\n" +
				"https://go.dev\n" +
				"api https://api.example.com/health\n" +
				"https://example.com/x 204\n" +
				"   site   http://localhost:8080/  2xx,301   # trailing comment\n" +
				"multi https://a.example 200,204\n",
			want: []want{
				{"go.dev", "https://go.dev", "2xx"},
				{"api", "https://api.example.com/health", "2xx"},
				{"example.com", "https://example.com/x", "204"},
				{"site", "http://localhost:8080/", "2xx,301"},
				{"multi", "https://a.example", "200,204"},
			},
		},
		{name: "empty", in: "\n# only comments\n   \n"},
		{name: "crlf", in: "a https://a.example\r\nb https://b.example 3xx\r\n", want: []want{
			{"a", "https://a.example", "2xx"}, {"b", "https://b.example", "3xx"},
		}},
		{name: "bad scheme", in: "ok https://a.example\nftp://x\n", wantErr: `t.txt:2: invalid URL "ftp://x": unsupported scheme`},
		{name: "missing scheme", in: "\n\nexample.com 200\n", wantErr: `t.txt:3: invalid URL "example.com": missing scheme`},
		{name: "name without url", in: "justaname\n", wantErr: "t.txt:1: invalid URL \"justaname\""},
		{name: "bad status", in: "a https://a.example abc\n", wantErr: `t.txt:1: invalid status "abc"`},
		{name: "bad status 2 fields", in: "https://a.example 99\n", wantErr: `t.txt:1: invalid status "99"`},
		{name: "too many fields", in: "a https://a.example 200 extra\n", wantErr: "t.txt:1: too many fields (4)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTargets(strings.NewReader(tt.in), "t.txt", def)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d targets, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.Name != w.name || g.URL != w.url || g.Expect.String() != w.expect {
					t.Errorf("target %d = {%q %q %q}, want %+v", i, g.Name, g.URL, g.Expect, w)
				}
			}
		})
	}
}

func TestParseTargetsReadError(t *testing.T) {
	boom := errors.New("boom")
	_, err := ParseTargets(iotest.ErrReader(boom), "x.txt", Expect{})
	if !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "x.txt: ") {
		t.Fatalf("err = %v", err)
	}
}
