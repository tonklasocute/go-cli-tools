package envcheck

import (
	"strings"
	"testing"
)

func TestGuessRules(t *testing.T) {
	tests := []struct{ key, value, want string }{
		{"PORT", "3000", "required,int"},
		{"RETRIES", "0", "required,int"},
		{"NEG", "-4", "required,int"},
		{"RATIO", "0.75", "required,float"},
		{"SMALL", ".5", "required,float"},
		{"EXP", "1.5e3", "required,float"},
		{"DEBUG", "false", "required,bool"},
		{"FEATURE", "On", "required,bool"},
		{"HOMEPAGE", "https://example.com", "required,url"},
		{"DATABASE_URL", "postgres://u:p@localhost/db", "required,url"},
		{"ADMIN", "ops@example.com", "required,email"},
		{"NAME", "orders-api", "required"},
		{"EMPTY", "", "required"},
		{"VERSION", "1.2.3", "required"},
		{"MAILTO", "mailto:ops@example.com", "required"},
		{"API_KEY", "12345", "required,secret"},
		{"DB_PASSWORD", "true", "required,secret"},
		{"github_token", "ghp_x", "required,secret"},
	}
	for _, tt := range tests {
		if got := GuessRules(tt.key, tt.value); got != tt.want {
			t.Errorf("GuessRules(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestGenerateSchema(t *testing.T) {
	src := "# App settings\nPORT=3000 # http port\nDEBUG=true\n\n# Secrets\nAPI_KEY=\"sk-abc123\"\nCERT=\"-----BEGIN-----\nxyz\n-----END-----\"\nexport HOMEPAGE=https://example.com\nPORT=4000\nbad line\nLONG_VARIABLE_NAME=hello\n"
	env := Parse([]byte(src))
	text, keys := GenerateSchema(env, ".env.local")
	want := "# Schema for envcheck, generated from .env.local by `envcheck init`.\n" +
		"# Values were stripped on purpose. Review the guessed rules before committing.\n" +
		"# Rules: required, secret, int, float, bool, url, email, enum=a|b, min=N, max=N, regex=<pattern> (last).\n" +
		"\n" +
		"# App settings\n" +
		"PORT=                # required,int\n" +
		"DEBUG=               # required,bool\n" +
		"\n" +
		"# Secrets\n" +
		"API_KEY=             # required,secret\n" +
		"CERT=                # required\n" +
		"HOMEPAGE=            # required,url\n" +
		"\n" +
		"LONG_VARIABLE_NAME=  # required\n"
	if text != want {
		t.Fatalf("schema =\n%s\nwant\n%s", text, want)
	}
	if len(keys) != 6 || keys[0] != (GeneratedKey{Key: "PORT", Rules: "required,int", Line: 2}) {
		t.Fatalf("keys = %+v", keys)
	}
	for _, secret := range []string{"sk-abc123", "3000", "BEGIN", "http port", "example.com"} {
		if strings.Contains(text, secret) {
			t.Errorf("generated schema leaks %q", secret)
		}
	}
	// The generated schema must itself be a valid schema that accepts the
	// source file (except for nothing: every key is present).
	s, err := ParseSchema([]byte(text), "generated")
	if err != nil {
		t.Fatalf("generated schema does not parse: %v", err)
	}
	sum := Summarize(Validate(s, env, Options{}))
	if sum.Errors != 1 || sum.OK != 6 { // the only error is the malformed line
		t.Fatalf("round trip summary = %+v", sum)
	}
}

func TestGenerateSchemaEmpty(t *testing.T) {
	text, keys := GenerateSchema(Parse(nil), "x")
	if len(keys) != 0 || !strings.HasPrefix(text, "# Schema for envcheck") {
		t.Fatalf("text=%q keys=%v", text, keys)
	}
}
