package diff3

import "testing"

func BenchmarkMergeNoConflict(b *testing.B) {
	base := []byte("a\nb\nc\nd\n")
	ours := []byte("a\nb ours\nc\nd\n")
	theirs := []byte("a\nb\nc\nd theirs\n")

	b.SetBytes(int64(len(base)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := Merge(base, ours, theirs)
		if result.HasConflicts {
			b.Fatal("expected clean merge")
		}
	}
}

func BenchmarkMergeConflict(b *testing.B) {
	base := []byte("a\nb\nc\nd\n")
	ours := []byte("a\nb ours\nc\nd\n")
	theirs := []byte("a\nb theirs\nc\nd\n")

	b.SetBytes(int64(len(base)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := Merge(base, ours, theirs)
		if !result.HasConflicts {
			b.Fatal("expected conflict")
		}
	}
}
