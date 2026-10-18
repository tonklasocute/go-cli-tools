package jsonformat

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// Options controls how Format renders JSON.
type Options struct {
	// Indent is the indentation unit (e.g. "  " or "\t"). An empty Indent
	// produces compact (minified) output, one value per line.
	Indent string
	// SortKeys sorts object keys. It requires buffering each top-level value.
	SortKeys bool
	// Query, when non-nil, selects a sub-value of every top-level value.
	Query Path
	// Raw prints selected string values without quotes or escapes (like
	// jq -r). Other values are printed as JSON.
	Raw bool
	// Theme enables syntax highlighting when it has color enabled. nil means
	// plain output.
	Theme *ui.Theme
}

// buffered reports whether values must be materialised as a tree.
func (o Options) buffered() bool { return o.SortKeys || o.Query != nil }

// Format reads every JSON value from r and writes it to w according to opt.
// The default path streams tokens, so memory use does not depend on the
// input size; SortKeys and Query buffer one top-level value at a time.
//
// It returns a *SyntaxError for malformed input, a *QueryError when the
// query path does not exist, ctx.Err() when canceled, or an I/O error.
// Values completed before an error are written out. The output of the value
// that failed is discarded unless it was too large to hold back, in which
// case the partial output is terminated with a newline.
func Format(ctx context.Context, r io.Reader, w io.Writer, opt Options) error {
	out := &outBuf{dst: w}
	e := newEmitter(out, opt.Indent, opt.Theme)
	sc := NewScanner(ctx, r)
	var err error
	if opt.buffered() {
		err = formatTree(sc, e, opt)
	} else {
		err = formatStream(sc, e)
	}
	if err != nil && !out.discardPartial() {
		e.abort()
	}
	if ferr := out.flush(); err == nil {
		err = ferr
	}
	return err
}

func formatStream(sc *Scanner, e *emitter) error {
	for {
		tok, err := sc.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		e.token(tok)
		if e.depth == 0 && !e.afterKey {
			e.w.commit()
		}
	}
}

func formatTree(sc *Scanner, e *emitter, opt Options) error {
	for {
		v, err := ReadValue(sc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if opt.SortKeys {
			v.SortKeys()
		}
		if opt.Query != nil {
			if v, err = Lookup(v, opt.Query); err != nil {
				return err
			}
		}
		if opt.Raw && v.Kind == String {
			e.w.str(strings.ToValidUTF8(v.Text, string(utf8.RuneError)))
			e.w.byte1('\n')
		} else {
			e.value(v)
		}
		e.w.commit()
	}
}

// Validate checks that r contains one or more well-formed JSON values. It
// returns nil, a *SyntaxError, ctx.Err() or an I/O error.
func Validate(ctx context.Context, r io.Reader) error {
	sc := NewScanner(ctx, r)
	for {
		if _, err := sc.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
