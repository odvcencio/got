package entity

import "testing"

func TestIsDataFormat(t *testing.T) {
	tests := []struct {
		lang string
		want bool
	}{
		{"json", true},
		{"json5", true},
		{"yaml", true},
		{"toml", true},
		{"ini", true},
		{"csv", true},
		{"go", false},
		{"python", false},
		{"typescript", false},
		{"c", false},
		{"rust", false},
		{"html", false},
		{"css", false},
		{"sql", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsDataFormat(tt.lang); got != tt.want {
			t.Errorf("IsDataFormat(%q) = %v, want %v", tt.lang, got, tt.want)
		}
	}
}

func TestShouldSkipExtraction(t *testing.T) {
	tests := []struct {
		lang  string
		size  int64
		force bool
		want  bool
	}{
		// Code files: never skip
		{"go", 10 * 1024 * 1024, false, false},
		{"python", 1, false, false},
		// Data below threshold: don't skip
		{"json", 100 * 1024, false, false},
		{"yaml", 200 * 1024, false, false},
		// Data above threshold: skip
		{"json", 300 * 1024, false, true},
		{"toml", 1024 * 1024, false, true},
		// Data above threshold with force: don't skip
		{"json", 300 * 1024, true, false},
		// Exact threshold: don't skip (<=)
		{"json", DataFormatSizeThreshold, false, false},
		// One byte over: skip
		{"json", DataFormatSizeThreshold + 1, false, true},
	}
	for _, tt := range tests {
		if got := ShouldSkipExtraction(tt.lang, tt.size, tt.force); got != tt.want {
			t.Errorf("ShouldSkipExtraction(%q, %d, %v) = %v, want %v",
				tt.lang, tt.size, tt.force, got, tt.want)
		}
	}
}
