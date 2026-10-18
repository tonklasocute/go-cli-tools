package jsonformat

import "io"

const (
	flushAt   = 64 << 10 // flush complete values once this much output is pending
	holdLimit = 4 << 20  // max output held back for an incomplete value
)

// outBuf collects formatted output. Output of the current top-level value
// is held back until the value is complete (up to holdLimit), so a syntax
// error usually discards the partial value instead of printing half of it.
// Write errors are sticky and reported by flush.
type outBuf struct {
	dst       io.Writer
	buf       []byte
	committed int  // prefix of buf holding complete values
	spilled   bool // part of the current value has already been written to dst
	err       error
}

func (o *outBuf) bytes(p []byte) {
	o.buf = append(o.buf, p...)
	if len(o.buf) >= holdLimit {
		o.spill()
	}
}

func (o *outBuf) str(s string) {
	o.buf = append(o.buf, s...)
	if len(o.buf) >= holdLimit {
		o.spill()
	}
}

func (o *outBuf) byte1(c byte) { o.buf = append(o.buf, c) }

func (o *outBuf) spill() {
	o.spilled = o.spilled || len(o.buf) > o.committed
	o.writeOut()
}

// commit marks everything written so far as complete values.
func (o *outBuf) commit() {
	o.committed = len(o.buf)
	o.spilled = false
	if len(o.buf) >= flushAt {
		o.writeOut()
	}
}

// discardPartial drops the held-back output of an incomplete value. It
// reports false when part of that value was already written out.
func (o *outBuf) discardPartial() bool {
	if o.spilled {
		return false
	}
	o.buf = o.buf[:o.committed]
	return true
}

// flush writes all pending output and returns the first write error.
func (o *outBuf) flush() error {
	o.writeOut()
	return o.err
}

// writeOut writes the buffer to dst, recording the first error.
func (o *outBuf) writeOut() {
	if len(o.buf) > 0 && o.err == nil {
		_, o.err = o.dst.Write(o.buf)
	}
	o.buf = o.buf[:0]
	o.committed = 0
}
