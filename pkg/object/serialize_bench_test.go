package object

import (
	"bytes"
	"testing"
)

var marshalBlobBenchmarkSink []byte
var unmarshalBlobBenchmarkSink *Blob

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

func BenchmarkUnmarshalBlobLarge(b *testing.B) {
	payload := bytes.Repeat([]byte{'x'}, 8<<20)

	b.Run("copy-default", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(payload)))
		for i := 0; i < b.N; i++ {
			out, err := UnmarshalBlob(payload)
			if err != nil {
				b.Fatalf("UnmarshalBlob: %v", err)
			}
			unmarshalBlobBenchmarkSink = out
		}
	})

	b.Run("no-copy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(payload)))
		for i := 0; i < b.N; i++ {
			out, err := UnmarshalBlobNoCopy(payload)
			if err != nil {
				b.Fatalf("UnmarshalBlobNoCopy: %v", err)
			}
			unmarshalBlobBenchmarkSink = out
		}
	})
}
