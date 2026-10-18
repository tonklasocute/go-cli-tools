package envcheck

import (
	"strings"
	"testing"
)

func TestCheckValue(t *testing.T) {
	tests := []struct {
		rules string
		value string
		want  string // substring of the joined problems; "" means valid
	}{
		{"int", "42", ""},
		{"int", "-7", ""},
		{"int", "4.2", `"4.2" is not an integer`},
		{"int", "99999999999999999999", "is not an integer"},
		{"int,min=1,max=65535", "70000", `"70000" is above the maximum 65535`},
		{"int,min=1,max=65535", "0", `"0" is below the minimum 1`},
		{"int,min=1,max=65535", "abc", "is not an integer"},
		{"float", "2.5", ""},
		{"float", "1e3", ""},
		{"float", "NaN", "is not a number"},
		{"float", "inf", "is not a number"},
		{"float,min=0.1,max=60", "0.05", "below the minimum 0.1"},
		{"bool", "TRUE", ""},
		{"bool", "Off", ""},
		{"bool", "1", ""},
		{"bool", "yes", ""},
		{"bool", "maybe", "is not a boolean"},
		{"url", "https://example.com/x?y=1", ""},
		{"url", "postgres://u:p@db:5432/app", ""},
		{"url", "example.com", "is not an absolute URL"},
		{"url", "/relative/path", "is not an absolute URL"},
		{"url", "http://", "is not an absolute URL"},
		{"email", "ops@example.com", ""},
		{"email", "ops@@example.com", "is not a valid email address"},
		{"email", "Ops <ops@example.com>", "is not a valid email address"},
		{"email", "not-an-email", "is not a valid email address"},
		{"enum=debug|info", "info", ""},
		{"enum=debug|info", "INFO", `"INFO" is not one of debug|info`},
		{`regex=^v\d+\.\d+\.\d+$`, "v1.2.3", ""},
		{`regex=^v\d+\.\d+\.\d+$`, "1.2.3", `"1.2.3" does not match ^v\d+\.\d+\.\d+$`},
		{"regex=b", "abc", ""}, // unanchored
		{"min=3,max=5", "abcd", ""},
		{"min=3", "ab", `"ab" is 2 characters, shorter than 3`},
		{"max=3", "日本語です", `is 5 characters, longer than 3`},
		{"int,enum=1|2,max=1", "2", `"2" is above the maximum 1`},
		{"url,regex=^https", "ftp://x.y", "does not match ^https"},
	}
	for _, tt := range tests {
		t.Run(tt.rules+"="+tt.value, func(t *testing.T) {
			f := &Field{Key: "K", Type: TypeString}
			if err := parseRules(f, tt.rules); err != nil {
				t.Fatal(err)
			}
			got := strings.Join(checkValue(f, tt.value), "; ")
			if tt.want == "" && got != "" {
				t.Fatalf("unexpected problems: %s", got)
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("problems = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestCheckValueSecretNeverShown(t *testing.T) {
	const val = "hunter2hunter2"
	for _, rules := range []string{
		"secret,int", "secret,float", "secret,bool", "secret,url", "secret,email",
		"secret,enum=a|b", "secret,regex=^x$", "secret,min=20", "secret,max=3", "secret,int,max=1",
	} {
		f := &Field{Key: "K", Type: TypeString}
		if err := parseRules(f, rules); err != nil {
			t.Fatal(err)
		}
		problems := strings.Join(checkValue(f, val), "; ")
		if problems == "" {
			t.Fatalf("%s: expected a problem", rules)
		}
		if strings.Contains(problems, "hunter") || strings.Contains(problems, "14") {
			t.Errorf("%s: secret value or length leaked: %q", rules, problems)
		}
	}
}

const validatorSchema = `APP=x            # required,min=2
PORT=3000        # required,int,min=1,max=65535
LEVEL=info       # enum=debug|info
TOKEN=           # required,secret
OPTIONAL=dflt
OPT_SECRET=pw    # secret
EMPTY_OPT=       # int
EMPTY_REQ=       # required
`

func TestValidate(t *testing.T) {
	s := mustSchema(t, validatorSchema)
	env := Parse([]byte("APP=web\nPORT=70000\nLEVEL=info\nTOKEN=s3cr3t-value\nEMPTY_OPT=\nEMPTY_REQ=\nEXTRA=1\nbroken line\nEXTRA=2\nAPP=api\n"))
	got := Validate(s, env, Options{})
	type row struct {
		key    string
		status Status
		line   int
		msg    string
	}
	want := []row{
		{"APP", StatusOK, 10, "length ≥ 2"},
		{"APP", StatusWarn, 10, "duplicate key on lines 1, 10; the last one (line 10) wins"},
		{"PORT", StatusError, 2, `"70000" is above the maximum 65535`},
		{"LEVEL", StatusOK, 3, "one of debug|info"},
		{"TOKEN", StatusOK, 4, "secret"},
		{"OPTIONAL", StatusSkip, 0, `optional, not set (example: "dflt")`},
		{"OPT_SECRET", StatusSkip, 0, "optional, not set (example: " + Mask + ")"},
		{"EMPTY_OPT", StatusSkip, 5, "optional, empty"},
		{"EMPTY_REQ", StatusError, 6, "required but empty"},
		{"EXTRA", StatusWarn, 9, "not declared in the schema"},
		{"EXTRA", StatusWarn, 9, "duplicate key on lines 7, 9; the last one (line 9) wins"},
		{"", StatusError, 8, "malformed line: expected '=' after key \"broken\""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Key != w.key || g.Status != w.status || g.Line != w.line || g.Message != w.msg {
			t.Errorf("result %d = %+v, want %+v", i, g, w)
		}
		if g.Value != nil {
			t.Errorf("result %d: value set without ShowValues", i)
		}
	}
	sum := Summarize(got)
	if sum != (Summary{OK: 3, Warnings: 3, Errors: 3, Skipped: 3}) {
		t.Errorf("summary = %+v", sum)
	}
}

func TestValidateMissingRequiredAndShowValues(t *testing.T) {
	s := mustSchema(t, validatorSchema)
	env := Parse([]byte("PORT=8080\nLEVEL=debug\nAPI_TOKEN=leaky\nOTHER=plain\n"))
	got := Validate(s, env, Options{ShowValues: true})
	byKey := map[string]Result{}
	for _, r := range got {
		byKey[r.Key] = r
	}
	if r := byKey["APP"]; r.Status != StatusError || r.Message != "required but not set" || r.Line != 0 || r.Value != nil {
		t.Errorf("APP = %+v", r)
	}
	if r := byKey["TOKEN"]; r.Status != StatusError || r.Message != "required but not set" {
		t.Errorf("TOKEN = %+v", r)
	}
	if r := byKey["PORT"]; r.Value == nil || *r.Value != "8080" {
		t.Errorf("PORT value = %v", r.Value)
	}
	if r := byKey["API_TOKEN"]; r.Status != StatusWarn || r.Value == nil || *r.Value != Mask {
		t.Errorf("extra secret-looking key must be masked: %+v", r)
	}
	if r := byKey["OTHER"]; r.Value == nil || *r.Value != "plain" {
		t.Errorf("OTHER = %+v", r)
	}
}

func TestDescribe(t *testing.T) {
	tests := map[string]string{
		"":                           "string",
		"int,min=1,max=65535":        "int 1–65535",
		"float,min=0.5":              "float ≥ 0.5",
		"int,max=9":                  "int ≤ 9",
		"min=3,max=32":               "length 3–32",
		"url,secret":                 "url, secret",
		"enum=a|b":                   "one of a|b",
		"required,regex=^x,y$":       "matches ^x,y$",
		"email,required":             "email",
		"bool,enum=true|false,max=5": "bool length ≤ 5, one of true|false",
	}
	for rules, want := range tests {
		f := &Field{Type: TypeString}
		if err := parseRules(f, rules); err != nil {
			t.Fatal(err)
		}
		if got := Describe(f); got != want {
			t.Errorf("Describe(%q) = %q, want %q", rules, got, want)
		}
	}
}

func TestLooksSecret(t *testing.T) {
	for k, want := range map[string]bool{
		"API_KEY": true, "apikey": true, "DB_PASSWORD": true, "GITHUB_TOKEN": true, "JWT_SECRET": true,
		"PRIVATE_KEY_PATH": true, "AWS_CREDENTIALS": true, "PASSWD": true, "PORT": false, "HOST": false,
	} {
		if got := LooksSecret(k); got != want {
			t.Errorf("LooksSecret(%q) = %v", k, got)
		}
	}
}
