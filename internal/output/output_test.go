package output

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func TestPrinterEmit(t *testing.T) {
	type result struct {
		URL string `json:"url"`
		N   int    `json:"n"`
	}
	v := result{URL: "https://go.dev/?a=1&b=<2>", N: 3}
	errText := errors.New("boom")

	tests := []struct {
		name    string
		json    bool
		theme   *ui.Theme
		text    TextFunc
		want    string
		wantErr error
	}{
		{
			name: "json no html escaping",
			json: true,
			want: "{\n  \"url\": \"https://go.dev/?a=1&b=<2>\",\n  \"n\": 3\n}\n",
		},
		{
			name: "text with nil theme",
			text: func(w io.Writer, th *ui.Theme) error {
				if th == nil || th.Color() {
					t.Fatal("expected a non-nil colorless theme")
				}
				_, err := io.WriteString(w, "hello\n")
				return err
			},
			want: "hello\n",
		},
		{
			name:    "text error propagates",
			theme:   ui.NewTheme(false),
			text:    func(io.Writer, *ui.Theme) error { return errText },
			wantErr: errText,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Printer{W: &buf, JSON: tt.json, Theme: tt.theme}.Emit(v, tt.text)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
