package envcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// maxInput caps how much is read from a file or stdin.
const maxInput = 16 << 20

const stdinName = "<stdin>"

type options struct {
	envFile    string
	schemaFile string
	strict     bool
	showValues bool
}

// Report is the result of checking one .env file against a schema; it is
// what --json prints.
type Report struct {
	// EnvFile is the checked file ("<stdin>" for standard input).
	EnvFile string `json:"env_file"`
	// SchemaFile is the schema used.
	SchemaFile string `json:"schema_file"`
	// OK is true when there are no errors (and no warnings under --strict).
	OK bool `json:"ok"`
	// Strict reports whether warnings count as failures.
	Strict bool `json:"strict"`
	// Summary counts results by status.
	Summary Summary `json:"summary"`
	// Results lists every check in schema order, then extra keys, then
	// malformed lines.
	Results []Result `json:"results"`
}

const longHelp = `envcheck validates a .env file against a schema written as an annotated
.env.example, so missing keys, typos and bad values are caught before the
app starts. Values of keys marked secret are never printed.

Schema format: every assignment in .env.example declares a key; its inline
comment is a comma-separated list of rules:

  PORT=3000        # required,int,min=1,max=65535
  DATABASE_URL=    # required,url
  LOG_LEVEL=info   # enum=debug|info|warn|error
  DEBUG=false      # bool
  ADMIN_EMAIL=     # email
  API_KEY=         # required,secret
  RELEASE=         # regex=^v\d+\.\d+\.\d+$

Rules: required, secret, int, float, bool (true/false/1/0/yes/no/on/off),
url (absolute, with scheme and host), email, enum=a|b|c, regex=<pattern>,
min=N, max=N. min/max bound the number for int/float and the length in
characters for anything else. regex= consumes the rest of the annotation,
so patterns may contain commas: put it last. Patterns use Go RE2 syntax and
are unanchored unless you add ^ and $. A key without a comment is an
optional string; full-line comments are free-form documentation.

Checks: missing or empty required keys, type/enum/regex/min/max violations
and malformed lines are errors. Keys not in the schema and duplicate keys
(the last assignment wins) are warnings. Optional keys that are not set are
skipped.

The .env parser understands comments, "export " prefixes, single quotes
(literal), double quotes (escapes \n \r \t \" \\ \$), backticks, multiline
quoted values, CRLF and a UTF-8 BOM. Variable expansion (${VAR}) is not
supported: values are checked literally.

Use "envcheck init" to bootstrap a schema from an existing .env.

Exit codes:
  0    the .env file is valid (warnings allowed unless --strict)
  1    validation failed
  2    usage error, unreadable file or invalid schema
  130  interrupted`

// NewCommand builds the envcheck command bound to the given streams.
func NewCommand(streams cliutil.IO) *cobra.Command {
	c := &cliutil.Common{IO: streams}
	var o options
	cmd := &cobra.Command{
		Use:   "envcheck [flags] [ENV_FILE]",
		Short: "Validate .env files against an annotated .env.example schema",
		Long:  longHelp,
		Example: "  envcheck\n" +
			"  envcheck --env .env.production --schema .env.example\n" +
			"  envcheck --strict --show-values .env.staging\n" +
			"  cat .env | envcheck --env - --json\n" +
			"  envcheck init > .env.example\n" +
			"  envcheck init --env .env.local -o .env.example --force",
		Args: cliutil.WrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), c, cmd, &o, args)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.envFile, "env", "e", ".env", `.env file to check ("-" reads stdin)`)
	f.StringVarP(&o.schemaFile, "schema", "s", ".env.example", `schema file ("-" reads stdin)`)
	f.BoolVar(&o.strict, "strict", false, "treat warnings (unknown or duplicate keys) as failures")
	f.BoolVar(&o.showValues, "show-values", false, "add a VALUE column (secrets stay masked)")
	cmd.AddCommand(newInitCommand(c))
	return cliutil.NewRoot(c, cmd)
}

func run(ctx context.Context, c *cliutil.Common, cmd *cobra.Command, o *options, args []string) error {
	envPath, explicit := o.envFile, cmd.Flags().Changed("env")
	if len(args) == 1 {
		if explicit {
			return cliutil.UsageErrorf("give the .env file either as an argument or with --env, not both")
		}
		envPath, explicit = args[0], true
	}
	switch {
	case envPath == "" || o.schemaFile == "":
		return cliutil.UsageErrorf("--env and --schema must not be empty")
	case envPath == "-" && o.schemaFile == "-":
		return cliutil.UsageErrorf("only one of --env and --schema can read stdin")
	}

	schemaData, schemaName, err := readInput(ctx, c.IO.In, o.schemaFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !cmd.Flags().Changed("schema") {
			return fmt.Errorf("%w (pass --schema, or create one with 'envcheck init > .env.example')", err)
		}
		return err
	}
	schema, err := ParseSchema(schemaData, schemaName)
	if err != nil {
		return err
	}

	envData, envName, err := readInput(ctx, c.IO.In, envPath)
	if err != nil && errors.Is(err, fs.ErrNotExist) && !explicit && o.schemaFile != "-" && c.IO.StdinIsPiped() {
		// No .env here but something was piped in: check that instead.
		if data, name, serr := readInput(ctx, c.IO.In, "-"); serr == nil && len(data) > 0 {
			envData, envName, err = data, name, nil
		}
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	results := Validate(schema, Parse(envData), Options{ShowValues: o.showValues})
	sum := Summarize(results)
	rep := &Report{
		EnvFile:    envName,
		SchemaFile: schemaName,
		OK:         sum.Errors == 0 && (!o.strict || sum.Warnings == 0),
		Strict:     o.strict,
		Summary:    sum,
		Results:    results,
	}
	if rep.Results == nil {
		rep.Results = []Result{}
	}
	if err := c.Printer().Emit(rep, func(w io.Writer, th *ui.Theme) error {
		return renderReport(w, th, rep, o.showValues)
	}); err != nil {
		return err
	}
	if !rep.OK {
		return cliutil.ErrCheckFailed
	}
	return nil
}

// readInput reads path ("-" for in) and returns its content and display
// name. Reading stops early when ctx is canceled.
func readInput(ctx context.Context, in io.Reader, path string) ([]byte, string, error) {
	if path == "-" {
		if in == nil {
			return nil, stdinName, errors.New("no standard input available")
		}
		data, err := readAll(ctx, in)
		if err != nil {
			return nil, stdinName, fmt.Errorf("read stdin: %w", err)
		}
		return data, stdinName, nil
	}
	fh, err := os.Open(path)
	if err != nil {
		return nil, path, err
	}
	defer fh.Close()
	data, err := readAll(ctx, fh)
	if err != nil {
		return nil, path, fmt.Errorf("read %s: %w", path, err)
	}
	return data, path, nil
}

var errTooLarge = fmt.Errorf("input larger than %d MiB", maxInput>>20)

// readAll reads r up to maxInput bytes, returning ctx.Err() if ctx is
// canceled first.
func readAll(ctx context.Context, r io.Reader) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, maxInput+1))
		ch <- result{data, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err == nil && len(res.data) > maxInput {
			return nil, errTooLarge
		}
		return res.data, res.err
	}
}
