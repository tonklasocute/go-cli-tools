package envcheck

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

type initOptions struct {
	envFile string
	output  string
	force   bool
}

// InitReport is what "envcheck init --json" prints.
type InitReport struct {
	// EnvFile is the source .env file.
	EnvFile string `json:"env_file"`
	// Output is the file written, or empty when the schema went to stdout.
	Output string `json:"output"`
	// Keys lists the generated keys and their rules.
	Keys []GeneratedKey `json:"keys"`
	// Skipped lists malformed source lines that were left out.
	Skipped []LineError `json:"skipped"`
	// Schema is the generated schema text.
	Schema string `json:"schema"`
}

func newInitCommand(c *cliutil.Common) *cobra.Command {
	var o initOptions
	cmd := &cobra.Command{
		Use:   "init [flags] [ENV_FILE]",
		Short: "Generate a .env.example schema from an existing .env",
		Long: `init writes a schema for an existing .env file. Every key is marked
required, values are stripped (they may be secrets), and a type is guessed
from each value: bool, int, float, url or email. Keys whose name looks like
a credential (secret, password, token, api key, private, credential) become
"required,secret". Full-line comments and blank-line grouping are kept;
inline comments are dropped. Review the result before committing it.

The schema goes to stdout unless -o is given; -o refuses to overwrite an
existing file without --force.

Exit codes:
  0    schema generated
  2    usage error, unreadable .env or output file already exists
  130  interrupted`,
		Example: "  envcheck init > .env.example\n" +
			"  envcheck init --env .env.local -o .env.example\n" +
			"  envcheck init -o .env.example --force\n" +
			"  cat .env | envcheck init --env -",
		Args: cliutil.WrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if cmd.Flags().Changed("env") {
					return cliutil.UsageErrorf("give the .env file either as an argument or with --env, not both")
				}
				o.envFile = args[0]
			}
			return runInit(cmd, c, &o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.envFile, "env", "e", ".env", `.env file to read ("-" reads stdin)`)
	f.StringVarP(&o.output, "output", "o", "", "write the schema to this file instead of stdout")
	f.BoolVar(&o.force, "force", false, "overwrite the output file if it exists")
	return cmd
}

func runInit(cmd *cobra.Command, c *cliutil.Common, o *initOptions) error {
	ctx := cmd.Context()
	if o.envFile == "" {
		return cliutil.UsageErrorf("--env must not be empty")
	}
	if o.output == "-" {
		o.output = ""
	}
	data, name, err := readInput(ctx, c.IO.In, o.envFile)
	if err != nil {
		return err
	}
	env := Parse(data)
	text, keys := GenerateSchema(env, name)
	if err := ctx.Err(); err != nil {
		return err
	}
	eth := c.ErrTheme()
	for _, pe := range env.Errors {
		fmt.Fprintf(c.IO.Err, "%s %s:%d: skipped malformed line: %s\n", eth.Warn(ui.SymWarn), name, pe.Line, pe.Msg)
	}
	if len(keys) == 0 {
		fmt.Fprintf(c.IO.Err, "%s no keys found in %s\n", eth.Warn(ui.SymWarn), name)
	}
	if o.output != "" {
		if err := writeSchema(o.output, text, o.force); err != nil {
			return err
		}
	}
	rep := InitReport{EnvFile: name, Output: o.output, Keys: keys, Skipped: env.Errors, Schema: text}
	if rep.Keys == nil {
		rep.Keys = []GeneratedKey{}
	}
	if rep.Skipped == nil {
		rep.Skipped = []LineError{}
	}
	return c.Printer().Emit(rep, func(w io.Writer, th *ui.Theme) error {
		if o.output == "" {
			_, err := io.WriteString(w, text)
			return err
		}
		_, err := fmt.Fprintf(w, "%s wrote %s to %s\n", th.Success(ui.SymOK), ui.Plural(len(keys), "key", "keys"), o.output)
		return err
	})
}

// writeSchema creates path with text, refusing to replace an existing file
// unless force is set.
func writeSchema(path, text string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	// The schema holds no values and is meant to be committed, so it is
	// world-readable like any other source file.
	fh, err := os.OpenFile(path, flags, 0o644) //nolint:gosec // G302/G306: non-secret file meant to be shared
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
		return err
	}
	if _, err := io.WriteString(fh, text); err != nil {
		_ = fh.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return fh.Close()
}
