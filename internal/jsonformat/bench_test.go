package jsonformat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
)

// benchInput builds roughly size bytes of realistic JSON.
func benchInput(size int) []byte {
	var b bytes.Buffer
	b.WriteString("[\n")
	for i := 0; b.Len() < size; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"user-%d","email":"user%d@example.com","active":%t,"score":%d.%d,`+
			`"tags":["alpha","beta","สวัสดี"],"address":{"city":"Bangkok","zip":"10%03d","geo":[13.7563,100.5018]},"note":null}`,
			i, i, i, i%2 == 0, i%100, i%7, i%1000)
	}
	b.WriteString("\n]\n")
	return b.Bytes()
}

func benchmarkFormat(b *testing.B, opt Options) {
	data := benchInput(10 << 20)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Format(context.Background(), bytes.NewReader(data), io.Discard, opt); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormatPretty10MB(b *testing.B) { benchmarkFormat(b, Options{Indent: "  "}) }
func BenchmarkFormatMinify10MB(b *testing.B) { benchmarkFormat(b, Options{}) }
func BenchmarkFormatSortKeys10MB(b *testing.B) {
	benchmarkFormat(b, Options{Indent: "  ", SortKeys: true})
}

func BenchmarkValidate10MB(b *testing.B) {
	data := benchInput(10 << 20)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Validate(context.Background(), bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
