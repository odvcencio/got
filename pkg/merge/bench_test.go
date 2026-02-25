package merge

import "testing"

func BenchmarkMergeFilesStructuralNoConflict(b *testing.B) {
	base := []byte(`package main

func alpha() int {
	return 1
}

func beta() int {
	return 2
}
`)
	ours := []byte(`package main

func alpha() int {
	return 10
}

func beta() int {
	return 2
}
`)
	theirs := []byte(`package main

func alpha() int {
	return 1
}

func beta() int {
	return 20
}
`)

	b.SetBytes(int64(len(base)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := MergeFiles("main.go", base, ours, theirs)
		if err != nil {
			b.Fatalf("MergeFiles: %v", err)
		}
		if result.HasConflicts {
			b.Fatal("expected clean merge")
		}
	}
}

func BenchmarkMergeFilesTextFallback(b *testing.B) {
	base := []byte("# Title\n\nline one\nline two\n")
	ours := []byte("# Title\n\nline one (ours)\nline two\n")
	theirs := []byte("# Title\n\nline one\nline two (theirs)\n")

	b.SetBytes(int64(len(base)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := MergeFiles("README.md", base, ours, theirs)
		if err != nil {
			b.Fatalf("MergeFiles: %v", err)
		}
		if result.HasConflicts {
			b.Fatal("expected clean merge in fallback")
		}
	}
}
