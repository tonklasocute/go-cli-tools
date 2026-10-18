package envcheck

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func mustSchema(t *testing.T, src string) *Schema {
	t.Helper()
	s, err := ParseSchema([]byte(src), "schema")
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func TestParseSchemaRules(t *testing.T) {
	src := "# header comment\n\n" +
		"PORT=3000            # required,int,min=1,max=65535\n" +
		"DATABASE_URL=        # required,url\n" +
		"LOG_LEVEL=info       # enum=debug | info|warn|error\n" +
		"DEBUG=false          # bool\n" +
		"RATIO=               # float, min=0.5\n" +
		"ADMIN_EMAIL=         # email\n" +
		"API_KEY=             # REQUIRED , Secret\n" +
		"RELEASE=             # regex=^v\\d+\\.\\d+\\.\\d+$\n" +
		"CSV=                 # required,regex=^[a-z]+(,[a-z]+){0,3}$\n" +
		"NAME=                # max=10\n" +
		"PLAIN=hello\n"
	s := mustSchema(t, src)
	keys := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		keys[i] = f.Key
	}
	want := []string{"PORT", "DATABASE_URL", "LOG_LEVEL", "DEBUG", "RATIO", "ADMIN_EMAIL", "API_KEY", "RELEASE", "CSV", "NAME", "PLAIN"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v", keys)
	}
	port := s.Field("PORT")
	if !port.Required || port.Type != TypeInt || *port.Min != 1 || *port.Max != 65535 || port.Line != 3 || port.Default != "3000" {
		t.Errorf("PORT = %+v", port)
	}
	if f := s.Field("DATABASE_URL"); f.Type != TypeURL || !f.Required {
		t.Errorf("DATABASE_URL = %+v", f)
	}
	if f := s.Field("LOG_LEVEL"); !reflect.DeepEqual(f.Enum, []string{"debug", "info", "warn", "error"}) || f.Required {
		t.Errorf("LOG_LEVEL = %+v", f)
	}
	if f := s.Field("DEBUG"); f.Type != TypeBool {
		t.Errorf("DEBUG = %+v", f)
	}
	if f := s.Field("RATIO"); f.Type != TypeFloat || *f.Min != 0.5 || f.Max != nil {
		t.Errorf("RATIO = %+v", f)
	}
	if f := s.Field("ADMIN_EMAIL"); f.Type != TypeEmail {
		t.Errorf("ADMIN_EMAIL = %+v", f)
	}
	if f := s.Field("API_KEY"); !f.Required || !f.Secret || f.Type != TypeString {
		t.Errorf("API_KEY = %+v", f)
	}
	if f := s.Field("RELEASE"); f.Pattern.String() != `^v\d+\.\d+\.\d+$` {
		t.Errorf("RELEASE pattern = %q", f.Pattern)
	}
	if f := s.Field("CSV"); !f.Required || f.Pattern.String() != `^[a-z]+(,[a-z]+){0,3}$` {
		t.Errorf("CSV = %+v (regex must consume commas)", f)
	}
	if f := s.Field("NAME"); *f.Max != 10 || f.Numeric() {
		t.Errorf("NAME = %+v", f)
	}
	if f := s.Field("PLAIN"); f.Required || f.Secret || f.Type != TypeString || f.Default != "hello" {
		t.Errorf("PLAIN = %+v", f)
	}
	if s.Field("NOPE") != nil {
		t.Error("unknown key should return nil")
	}
}

func TestParseSchemaErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"unknown rule", "\nA= # required,integer", `schema:2: A: unknown rule "integer"`},
		{"description comment", "A=1 # the port", `unknown rule "the port"`},
		{"invalid regex", "A= # regex=^(unclosed$", `schema:1: A: invalid regex "^(unclosed$"`},
		{"bad min", "A= # int,min=abc", "min=abc is not a number"},
		{"nan max", "A= # float,max=NaN", "max=NaN is not a number"},
		{"min greater than max", "A= # int,min=10,max=1", "min=10 is greater than max=1"},
		{"negative length", "A= # min=-1", "min=-1 must be a non-negative whole number"},
		{"fractional length", "A= # max=2.5", "max=2.5 must be a non-negative whole number"},
		{"conflicting types", "A= # int,url", `conflicting types "int" and "url"`},
		{"duplicate rule", "A= # required,required", `rule "required" given more than once`},
		{"flag with value", "A= # required=yes", `rule "required" takes no value`},
		{"missing value", "A= # enum=", `rule "enum" needs a value`},
		{"empty enum choice", "A= # enum=a||b", "contains an empty choice"},
		{"duplicate key", "A=\nB=\nA=", "schema:3: duplicate key A (first declared on line 1)"},
		{"malformed line", "A=1\n-x valid\n", "schema:2: expected KEY=VALUE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSchema([]byte(tt.src), "schema")
			var se *SchemaError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *SchemaError", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %q, want to contain %q", err, tt.want)
			}
		})
	}
}

func TestSchemaErrorListsAllProblemsInOrder(t *testing.T) {
	_, err := ParseSchema([]byte("B= # bogus\nok line\nA= # int,min=x\n"), "s.env")
	want := "invalid schema:\n  s.env:1: B: unknown rule \"bogus\" (known rules: required, secret, int, float, bool, url, email, enum, regex, min, max; put descriptions on their own comment line)\n" +
		"  s.env:2: expected '=' after key \"ok\"\n" +
		"  s.env:3: A: min=x is not a number"
	if err == nil || err.Error() != want {
		t.Fatalf("err =\n%v\nwant\n%s", err, want)
	}
}

func TestParseSchemaEmptyAnnotationTokens(t *testing.T) {
	s := mustSchema(t, "A= # required, ,int,\nB= #   \n")
	if f := s.Field("A"); !f.Required || f.Type != TypeInt {
		t.Errorf("A = %+v", f)
	}
	if f := s.Field("B"); f.Required || f.Type != TypeString {
		t.Errorf("B = %+v", f)
	}
}
