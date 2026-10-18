package jsonformat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/output"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

const stdinName = "<stdin>"

type options struct {
	indent    int
	indentSet bool
	tab       bool
	minify    bool
	sortKeys  bool
	check     bool
	write     bool
	query     string
	raw       bool
}

// NewCommand builds the jsonformat command bound to the given streams.
func NewCommand(io cliutil.IO) *cobra.Command {
	c := &cliutil.Common{IO: io}
	var o options
	cmd := &cobra.Command{
		Use:   "jsonformat [flags] [file ...]",
		Short: "Pretty-print, minify, validate and query JSON",
		Long: `jsonformat pretty-prints, minifies, validates and queries JSON.

Input comes from the files given as arguments, or from standard input when
no file is given (or for "-"). Concatenated top-level values (NDJSON) are
formatted one after another. Formatting is streamed, so large files are
processed in constant memory; --sort-keys and --query hold one top-level
value at a time. Numbers are copied exactly as written (big integers and
long decimals never lose precision) and <, > and & are never escaped.

Output is syntax-highlighted when stdout is a terminal. With several files,
each one is preceded by a "==> file <==" header.

Invalid JSON is reported as file:line:col with the offending source line and
a caret under the exact column. --check only validates; combined with --json
it prints a machine-readable report. --write reformats files in place
atomically (temporary file + rename, permissions preserved, unchanged files
left untouched).

--query selects a value with a path such as a.b[0].c, .items[-1] or
["key.with.dots"]; add -r to print a selected string without quotes.
In formatting mode --json only disables color (the output already is JSON);
with --check or --write it switches to a JSON report.

Exit codes:
  0    success: all input is valid JSON
  1    invalid JSON, or the --query path does not exist
  2    usage or I/O error (missing file, --write with stdin, conflicting flags)
  130  interrupted (Ctrl+C)`,
		Example: `  jsonformat config.json
  curl -s https://api.github.com/repos/golang/go | jsonformat --query owner.login -r
  jsonformat --minify data.json > data.min.json
  jsonformat --sort-keys --indent 4 --write a.json b.json
  jsonformat --check configs/*.json
  cat events.ndjson | jsonformat --minify --sort-keys`,
		Args: cliutil.WrapArgs(cobra.ArbitraryArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.indentSet = cmd.Flags().Changed("indent")
			return run(cmd.Context(), c, &o, args)
		},
	}
	f := cmd.Flags()
	f.IntVar(&o.indent, "indent", 2, "spaces per indentation level (0-8; 0 is the same as --minify)")
	f.BoolVar(&o.tab, "tab", false, "indent with tabs")
	f.BoolVarP(&o.minify, "minify", "m", false, "compact output, one value per line")
	f.BoolVarP(&o.sortKeys, "sort-keys", "S", false, "sort object keys")
	f.BoolVar(&o.check, "check", false, "only validate; report each file and exit 1 if any is invalid")
	f.BoolVarP(&o.write, "write", "w", false, "rewrite the files in place (atomically)")
	f.StringVarP(&o.query, "query", "q", "", "print only the value at `path` (e.g. a.b[0].c)")
	f.BoolVarP(&o.raw, "raw", "r", false, "with --query, print strings without quotes")
	return cliutil.NewRoot(c, cmd)
}

// formatOptions validates flag combinations and builds Options.
func (o *options) formatOptions() (Options, error) {
	var opt Options
	switch {
	case o.indent < 0 || o.indent > 8:
		return opt, cliutil.UsageErrorf("--indent must be between 0 and 8, got %d", o.indent)
	case o.minify && o.indentSet:
		return opt, cliutil.UsageErrorf("--minify and --indent cannot be used together")
	case o.minify && o.tab:
		return opt, cliutil.UsageErrorf("--minify and --tab cannot be used together")
	case o.tab && o.indentSet:
		return opt, cliutil.UsageErrorf("--tab and --indent cannot be used together")
	case o.check && (o.write || o.query != "" || o.raw):
		return opt, cliutil.UsageErrorf("--check only validates; it cannot be combined with --write, --query or --raw")
	case o.write && (o.query != "" || o.raw):
		return opt, cliutil.UsageErrorf("--write cannot be combined with --query or --raw")
	case o.raw && o.query == "":
		return opt, cliutil.UsageErrorf("--raw requires --query")
	}
	switch {
	case o.tab:
		opt.Indent = "\t"
	case o.minify || o.indent == 0:
		opt.Indent = ""
	default:
		opt.Indent = strings.Repeat(" ", o.indent)
	}
	opt.SortKeys = o.sortKeys
	opt.Raw = o.raw
	if o.query != "" {
		p, err := ParsePath(o.query)
		if err != nil {
			return opt, cliutil.UsageError(err)
		}
		opt.Query = p
	}
	return opt, nil
}

func run(ctx context.Context, c *cliutil.Common, o *options, args []string) error {
	opt, err := o.formatOptions()
	if err != nil {
		return err
	}
	inputs := args
	if len(inputs) == 0 {
		if !c.IO.StdinIsPiped() {
			return cliutil.UsageErrorf("no input: pass one or more files, or pipe JSON on stdin")
		}
		inputs = []string{"-"}
	}
	if o.write {
		for _, in := range inputs {
			if in == "-" {
				return cliutil.UsageErrorf("--write needs files; it cannot rewrite standard input")
			}
		}
	}
	r := &runner{ctx: ctx, c: c, th: c.Theme(), eth: c.ErrTheme()}
	switch {
	case o.check:
		return r.check(inputs)
	case o.write:
		return r.write(opt, inputs)
	default:
		return r.format(opt, inputs)
	}
}

type runner struct {
	ctx   context.Context
	c     *cliutil.Common
	th    *ui.Theme // stdout
	eth   *ui.Theme // stderr
	worst int
	multi bool
}

// fileReport is one entry of the --json report for --check and --write.
type fileReport struct {
	File    string     `json:"file"`
	Valid   bool       `json:"valid"`
	Changed *bool      `json:"changed,omitempty"`
	Error   *errorInfo `json:"error,omitempty"`
}

type errorInfo struct {
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Offset  *int64 `json:"offset,omitempty"`
	Message string `json:"message"`
}

type report struct {
	Files []fileReport `json:"files"`
}

func displayName(name string) string {
	if name == "-" {
		return stdinName
	}
	return name
}

func (r *runner) open(name string) (io.ReadCloser, error) {
	if name == "-" {
		return io.NopCloser(r.c.IO.In), nil
	}
	return os.Open(name)
}

// classify records the exit code err calls for: 1 for invalid JSON or a
// missing query path, 2 for anything else (I/O errors).
func (r *runner) classify(err error) {
	code := cliutil.ExitUsage
	var se *SyntaxError
	var qe *QueryError
	if errors.As(err, &se) || errors.As(err, &qe) {
		code = cliutil.ExitFail
	}
	r.worst = max(r.worst, code)
}

func (r *runner) result() error {
	switch r.worst {
	case cliutil.ExitOK:
		return nil
	case cliutil.ExitFail:
		return cliutil.ErrCheckFailed
	default:
		return &cliutil.ExitError{Code: r.worst}
	}
}

func (r *runner) interrupted(err error) bool {
	return r.ctx.Err() != nil || errors.Is(err, context.Canceled)
}

// reportErr prints a non-JSON error report for one input on stderr.
func (r *runner) reportErr(name string, err error) {
	w := r.c.IO.Err
	var se *SyntaxError
	if errors.As(err, &se) {
		_ = se.Render(w, r.eth, se.Header(displayName(name), r.eth))
		return
	}
	msg := err.Error()
	var qe *QueryError
	if errors.As(err, &qe) && r.multi {
		msg = displayName(name) + ": " + msg
	}
	fmt.Fprintf(w, "jsonformat: %s %s\n", r.eth.Error("error:"), msg)
}

func errInfo(err error) *errorInfo {
	var se *SyntaxError
	if errors.As(err, &se) {
		off := se.Pos.Offset
		return &errorInfo{Line: se.Pos.Line, Col: se.Pos.Col, Offset: &off, Message: se.Msg}
	}
	return &errorInfo{Message: err.Error()}
}

func (r *runner) format(opt Options, inputs []string) error {
	opt.Theme = r.th
	r.multi = len(inputs) > 1
	bw := bufio.NewWriterSize(r.c.IO.Out, 64<<10)
	for i, name := range inputs {
		if r.multi {
			if i > 0 {
				fmt.Fprintln(bw)
			}
			fmt.Fprintln(bw, r.th.Muted("==> "+displayName(name)+" <=="))
		}
		err := r.formatOne(name, bw, opt)
		if ferr := bw.Flush(); err == nil {
			err = ferr
		}
		if err == nil {
			continue
		}
		if r.interrupted(err) {
			return r.ctx.Err()
		}
		r.classify(err)
		r.reportErr(name, err)
	}
	return r.result()
}

func (r *runner) formatOne(name string, w io.Writer, opt Options) error {
	in, err := r.open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	return Format(r.ctx, in, w, opt)
}

func (r *runner) check(inputs []string) error {
	var rep report
	invalid := 0
	for _, name := range inputs {
		err := func() error {
			in, err := r.open(name)
			if err != nil {
				return err
			}
			defer in.Close()
			return Validate(r.ctx, in)
		}()
		if err != nil && r.interrupted(err) {
			return r.ctx.Err()
		}
		fr := fileReport{File: displayName(name), Valid: err == nil}
		if err != nil {
			invalid++
			r.classify(err)
			fr.Error = errInfo(err)
		}
		rep.Files = append(rep.Files, fr)
		if !r.c.JSON {
			r.printCheck(name, err)
		}
	}
	if r.c.JSON {
		if err := output.JSON(r.c.IO.Out, rep); err != nil {
			return err
		}
	} else if len(inputs) > 1 {
		fmt.Fprintln(r.c.IO.Out, r.th.Muted(fmt.Sprintf("%s checked: %d valid, %d invalid",
			ui.Plural(len(inputs), "file", "files"), len(inputs)-invalid, invalid)))
	}
	return r.result()
}

func (r *runner) printCheck(name string, err error) {
	w, th := r.c.IO.Out, r.th
	disp := displayName(name)
	if err == nil {
		fmt.Fprintf(w, "%s %s: valid JSON\n", th.Success(ui.SymOK), disp)
		return
	}
	var se *SyntaxError
	if errors.As(err, &se) {
		header := th.Error(ui.SymFail) + " " + th.Bold(disp+":"+se.locString()+":") + " " + se.Msg
		_ = se.Render(w, th, header)
		return
	}
	fmt.Fprintf(w, "%s %s: %s\n", th.Error(ui.SymFail), disp, ioMessage(err))
}

// ioMessage strips the redundant "open <file>:" prefix from path errors.
func ioMessage(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func (r *runner) write(opt Options, inputs []string) error {
	var rep report
	for _, name := range inputs {
		changed, err := WriteFileAtomic(name, func(src io.Reader, dst io.Writer) error {
			return Format(r.ctx, src, dst, opt) // never colored: opt.Theme is nil
		})
		if err != nil && r.interrupted(err) {
			return r.ctx.Err()
		}
		fr := fileReport{File: name, Valid: err == nil}
		if err != nil {
			r.classify(err)
			fr.Error = errInfo(err)
			if !r.c.JSON {
				r.reportErr(name, err)
			}
		} else {
			fr.Changed = &changed
			if !r.c.JSON {
				if changed {
					fmt.Fprintf(r.c.IO.Out, "%s formatted %s\n", r.th.Success(ui.SymOK), name)
				} else {
					fmt.Fprintln(r.c.IO.Out, r.th.Muted(ui.SymInfo+" "+name+" already formatted"))
				}
			}
		}
		rep.Files = append(rep.Files, fr)
	}
	if r.c.JSON {
		if err := output.JSON(r.c.IO.Out, rep); err != nil {
			return err
		}
	}
	return r.result()
}
