package object

import (
	"bytes"
	"testing"
)

var marshalBlobBenchmarkSink []byte

func BenchmarkMarshalBlobLarge(b *testing.B) {
	payload := bytes.Repeat([]byte{'x'}, 8<<20)
	blob := &Blob{Data: payload}

	b.Run("zero-copy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(payload)))
		for i := 0; i < b.N; i++ {
			marshalBlobBenchmarkSink = MarshalBlob(blob)
		}
	})

	b.Run("legacy-copy-baseline", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(payload)))
		for i := 0; i < b.N; i++ {
			marshalBlobBenchmarkSink = append([]byte(nil), blob.Data...)
		}
	})
}
