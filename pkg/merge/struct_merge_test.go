package merge

import (
	"strings"
	"testing"
)

// --- Go struct field merge tests ---

func TestMergeGoStructFieldsUnion(t *testing.T) {
	base := `type Config struct {
	Host string
}`
	ours := `type Config struct {
	Host string
	Port int
}`
	theirs := `type Config struct {
	Host string
	Timeout int
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent field additions")
	}

	s := string(merged)
	for _, field := range []string{"Host string", "Port int", "Timeout int"} {
		if !strings.Contains(s, field) {
			t.Errorf("merged output missing field %q\nmerged:\n%s", field, s)
		}
	}
}

func TestMergeGoStructFieldsOneSidedDeletion(t *testing.T) {
	base := `type Config struct {
	Host string
	Port int
	Timeout int
}`
	ours := `type Config struct {
	Host string
	Timeout int
}`
	theirs := `type Config struct {
	Host string
	Port int
	Timeout int
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for one-sided deletion")
	}

	s := string(merged)
	if strings.Contains(s, "Port") {
		t.Errorf("merged output should not contain deleted field 'Port'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Host string") {
		t.Errorf("merged output missing field 'Host string'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Timeout int") {
		t.Errorf("merged output missing field 'Timeout int'\nmerged:\n%s", s)
	}
}

func TestMergeGoStructFieldsTypeConflict(t *testing.T) {
	base := `type Config struct {
	Host string
	Port int
}`
	ours := `type Config struct {
	Host string
	Port int32
}`
	theirs := `type Config struct {
	Host string
	Port uint16
}`

	_, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if !conflict {
		t.Fatal("expected conflict when both sides change same field to different types")
	}
}

func TestMergeGoStructFieldsTypeChangeOneSide(t *testing.T) {
	base := `type Config struct {
	Host string
	Port int
}`
	ours := `type Config struct {
	Host string
	Port int32
}`
	theirs := `type Config struct {
	Host string
	Port int
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict when only one side changes type")
	}

	s := string(merged)
	if !strings.Contains(s, "Port int32") {
		t.Errorf("merged output should have ours type change 'Port int32'\nmerged:\n%s", s)
	}
}

func TestMergeGoStructFieldsBothRemove(t *testing.T) {
	base := `type Config struct {
	Host string
	Port int
}`
	ours := `type Config struct {
	Host string
}`
	theirs := `type Config struct {
	Host string
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict when both remove same field")
	}

	s := string(merged)
	if strings.Contains(s, "Port") {
		t.Errorf("merged output should not contain field removed by both sides\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Host string") {
		t.Errorf("merged output should contain surviving field\nmerged:\n%s", s)
	}
}

func TestMergeGoStructFieldsEmbedded(t *testing.T) {
	base := `type Server struct {
	Name string
}`
	ours := `type Server struct {
	Name string
	io.Writer
}`
	theirs := `type Server struct {
	Name string
	sync.Mutex
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent embedded field additions")
	}

	s := string(merged)
	if !strings.Contains(s, "io.Writer") {
		t.Errorf("merged output missing embedded field 'io.Writer'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "sync.Mutex") {
		t.Errorf("merged output missing embedded field 'sync.Mutex'\nmerged:\n%s", s)
	}
}

func TestMergeGoStructFieldsPreservesOrder(t *testing.T) {
	base := `type Config struct {
	A string
	B string
	C string
}`
	ours := `type Config struct {
	A string
	B string
	C string
	D string
}`
	theirs := `type Config struct {
	A string
	B string
	C string
	E string
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict")
	}

	s := string(merged)
	// Base fields should come first, then new ones
	aIdx := strings.Index(s, "A string")
	bIdx := strings.Index(s, "B string")
	cIdx := strings.Index(s, "C string")
	dIdx := strings.Index(s, "D string")
	eIdx := strings.Index(s, "E string")

	if aIdx < 0 || bIdx < 0 || cIdx < 0 || dIdx < 0 || eIdx < 0 {
		t.Fatalf("missing fields in merged output\nmerged:\n%s", s)
	}

	if !(aIdx < bIdx && bIdx < cIdx && cIdx < dIdx && dIdx < eIdx) {
		t.Errorf("fields not in expected order (A < B < C < D < E)\nmerged:\n%s", s)
	}
}

func TestMergeGoStructFieldsWithTags(t *testing.T) {
	base := "type Config struct {\n\tHost string `json:\"host\"`\n}"
	ours := "type Config struct {\n\tHost string `json:\"host\"`\n\tPort int `json:\"port\"`\n}"
	theirs := "type Config struct {\n\tHost string `json:\"host\"`\n\tTimeout int `json:\"timeout\"`\n}"

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for fields with tags")
	}

	s := string(merged)
	if !strings.Contains(s, "Port int") {
		t.Errorf("merged output missing field 'Port int'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Timeout int") {
		t.Errorf("merged output missing field 'Timeout int'\nmerged:\n%s", s)
	}
}

// --- Rust struct field merge tests ---

func TestMergeRustStructFieldsUnion(t *testing.T) {
	base := `pub struct Config {
    host: String,
}`
	ours := `pub struct Config {
    host: String,
    port: u16,
}`
	theirs := `pub struct Config {
    host: String,
    timeout: u64,
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "rust")
	if conflict {
		t.Fatal("expected no conflict for independent Rust field additions")
	}

	s := string(merged)
	for _, field := range []string{"host: String", "port: u16", "timeout: u64"} {
		if !strings.Contains(s, field) {
			t.Errorf("merged output missing field %q\nmerged:\n%s", field, s)
		}
	}
}

func TestMergeRustStructFieldsOneSidedDeletion(t *testing.T) {
	base := `struct Point {
    x: f64,
    y: f64,
    z: f64,
}`
	ours := `struct Point {
    x: f64,
    y: f64,
}`
	theirs := `struct Point {
    x: f64,
    y: f64,
    z: f64,
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "rust")
	if conflict {
		t.Fatal("expected no conflict for one-sided Rust field deletion")
	}

	s := string(merged)
	if strings.Contains(s, "z:") {
		t.Errorf("merged output should not contain deleted field 'z'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "x: f64") {
		t.Errorf("merged output missing field 'x: f64'\nmerged:\n%s", s)
	}
}

func TestMergeRustStructFieldsTypeConflict(t *testing.T) {
	base := `pub struct Config {
    port: u16,
}`
	ours := `pub struct Config {
    port: u32,
}`
	theirs := `pub struct Config {
    port: i16,
}`

	_, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "rust")
	if !conflict {
		t.Fatal("expected conflict when both sides change same Rust field to different types")
	}
}

func TestMergeRustStructFieldsWithVisibility(t *testing.T) {
	base := `pub struct Config {
    pub host: String,
}`
	ours := `pub struct Config {
    pub host: String,
    pub port: u16,
}`
	theirs := `pub struct Config {
    pub host: String,
    pub timeout: u64,
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "rust")
	if conflict {
		t.Fatal("expected no conflict for Rust fields with pub visibility")
	}

	s := string(merged)
	if !strings.Contains(s, "pub port: u16") {
		t.Errorf("merged output missing field 'pub port: u16'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "pub timeout: u64") {
		t.Errorf("merged output missing field 'pub timeout: u64'\nmerged:\n%s", s)
	}
}

func TestMergeRustStructFieldsPreservesVisibility(t *testing.T) {
	base := `pub struct Config {
    pub host: String,
}`
	ours := `pub struct Config {
    pub host: String,
    pub(crate) port: u16,
}`
	theirs := `pub struct Config {
    pub host: String,
    timeout: u64,
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "rust")
	if conflict {
		t.Fatal("expected no conflict")
	}

	s := string(merged)
	if !strings.Contains(s, "pub struct Config") {
		t.Errorf("merged output should preserve pub struct visibility\nmerged:\n%s", s)
	}
}

// --- Edge cases ---

func TestMergeStructFieldsUnsupportedLanguage(t *testing.T) {
	_, conflict := MergeStructFields([]byte("{}"), []byte("{}"), []byte("{}"), "python")
	if !conflict {
		t.Fatal("expected conflict for unsupported language")
	}
}

func TestMergeGoStructFieldsEmptyBase(t *testing.T) {
	base := `type Config struct {
}`
	ours := `type Config struct {
	Host string
}`
	theirs := `type Config struct {
	Port int
}`

	merged, conflict := MergeStructFields([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for additions to empty struct")
	}

	s := string(merged)
	if !strings.Contains(s, "Host string") {
		t.Errorf("merged output missing field 'Host string'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Port int") {
		t.Errorf("merged output missing field 'Port int'\nmerged:\n%s", s)
	}
}

func TestIsStructDecl(t *testing.T) {
	tests := []struct {
		declKind string
		language string
		want     bool
	}{
		{"type_declaration", "go", true},
		{"struct_item", "rust", true},
		{"function_declaration", "go", false},
		{"struct_item", "go", false},
		{"type_declaration", "rust", false},
		{"class_definition", "python", false},
	}

	for _, tt := range tests {
		got := isStructDecl(tt.declKind, tt.language)
		if got != tt.want {
			t.Errorf("isStructDecl(%q, %q) = %v, want %v", tt.declKind, tt.language, got, tt.want)
		}
	}
}
