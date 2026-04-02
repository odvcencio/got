package merge

import (
	"strings"
	"testing"
)

// --- Go interface member merge tests ---

func TestMergeGoInterfaceMembersUnion(t *testing.T) {
	base := `type Reader interface {
	Read(p []byte) (n int, err error)
}`
	ours := `type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}`
	theirs := `type Reader interface {
	Read(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent method additions")
	}

	s := string(merged)
	for _, method := range []string{"Read(", "Close()", "Seek("} {
		if !strings.Contains(s, method) {
			t.Errorf("merged output missing method %q\nmerged:\n%s", method, s)
		}
	}
}

func TestMergeGoInterfaceMembersOneSidedDeletion(t *testing.T) {
	base := `type Handler interface {
	Handle(req Request) Response
	Close() error
	Reset()
}`
	ours := `type Handler interface {
	Handle(req Request) Response
	Reset()
}`
	theirs := `type Handler interface {
	Handle(req Request) Response
	Close() error
	Reset()
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for one-sided method deletion")
	}

	s := string(merged)
	if strings.Contains(s, "Close()") {
		t.Errorf("merged output should not contain deleted method 'Close()'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Handle(") {
		t.Errorf("merged output missing 'Handle('\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Reset()") {
		t.Errorf("merged output missing 'Reset()'\nmerged:\n%s", s)
	}
}

func TestMergeGoInterfaceMembersSignatureConflict(t *testing.T) {
	base := `type Handler interface {
	Handle(req Request) Response
}`
	ours := `type Handler interface {
	Handle(req Request) (Response, error)
}`
	theirs := `type Handler interface {
	Handle(ctx Context, req Request) Response
}`

	_, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if !conflict {
		t.Fatal("expected conflict when both sides change same method signature differently")
	}
}

func TestMergeGoInterfaceMembersSignatureChangeOneSide(t *testing.T) {
	base := `type Handler interface {
	Handle(req Request) Response
}`
	ours := `type Handler interface {
	Handle(req Request) (Response, error)
}`
	theirs := `type Handler interface {
	Handle(req Request) Response
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict when only one side changes signature")
	}

	s := string(merged)
	if !strings.Contains(s, "Handle(req Request) (Response, error)") {
		t.Errorf("merged output should have ours signature change\nmerged:\n%s", s)
	}
}

func TestMergeGoInterfaceMembersEmbedded(t *testing.T) {
	base := `type ReadWriter interface {
	io.Reader
}`
	ours := `type ReadWriter interface {
	io.Reader
	io.Closer
}`
	theirs := `type ReadWriter interface {
	io.Reader
	io.Writer
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent embedded interface additions")
	}

	s := string(merged)
	if !strings.Contains(s, "io.Reader") {
		t.Errorf("merged output missing 'io.Reader'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "io.Closer") {
		t.Errorf("merged output missing 'io.Closer'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "io.Writer") {
		t.Errorf("merged output missing 'io.Writer'\nmerged:\n%s", s)
	}
}

// --- TypeScript interface member merge tests ---

func TestMergeTSInterfaceMembersUnion(t *testing.T) {
	base := `interface Config {
  host: string;
}`
	ours := `interface Config {
  host: string;
  port: number;
}`
	theirs := `interface Config {
  host: string;
  timeout: number;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict for independent TS property additions")
	}

	s := string(merged)
	for _, prop := range []string{"host: string", "port: number", "timeout: number"} {
		if !strings.Contains(s, prop) {
			t.Errorf("merged output missing property %q\nmerged:\n%s", prop, s)
		}
	}
}

func TestMergeTSInterfaceMembersOneSidedDeletion(t *testing.T) {
	base := `interface Config {
  host: string;
  port: number;
  timeout: number;
}`
	ours := `interface Config {
  host: string;
  timeout: number;
}`
	theirs := `interface Config {
  host: string;
  port: number;
  timeout: number;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict for one-sided TS property deletion")
	}

	s := string(merged)
	if strings.Contains(s, "port") {
		t.Errorf("merged output should not contain deleted property 'port'\nmerged:\n%s", s)
	}
}

func TestMergeTSInterfaceMembersTypeConflict(t *testing.T) {
	base := `interface Config {
  port: number;
}`
	ours := `interface Config {
  port: string;
}`
	theirs := `interface Config {
  port: boolean;
}`

	_, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if !conflict {
		t.Fatal("expected conflict when both sides change same TS property type differently")
	}
}

func TestMergeTSInterfaceMembersTypeChangeOneSide(t *testing.T) {
	base := `interface Config {
  port: number;
}`
	ours := `interface Config {
  port: string;
}`
	theirs := `interface Config {
  port: number;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict when only one side changes type")
	}

	s := string(merged)
	if !strings.Contains(s, "port: string") {
		t.Errorf("merged output should have ours type change\nmerged:\n%s", s)
	}
}

func TestMergeTSInterfaceMembersExported(t *testing.T) {
	base := `export interface Config {
  host: string;
}`
	ours := `export interface Config {
  host: string;
  port: number;
}`
	theirs := `export interface Config {
  host: string;
  timeout: number;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict for exported TS interface")
	}

	s := string(merged)
	if !strings.HasPrefix(s, "export interface") {
		t.Errorf("merged output should preserve export keyword\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "port: number") {
		t.Errorf("merged output missing 'port: number'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "timeout: number") {
		t.Errorf("merged output missing 'timeout: number'\nmerged:\n%s", s)
	}
}

func TestMergeTSInterfaceMembersWithMethods(t *testing.T) {
	base := `interface Service {
  getName(): string;
}`
	ours := `interface Service {
  getName(): string;
  start(): void;
}`
	theirs := `interface Service {
  getName(): string;
  stop(): void;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict for independent TS method additions")
	}

	s := string(merged)
	for _, method := range []string{"getName()", "start()", "stop()"} {
		if !strings.Contains(s, method) {
			t.Errorf("merged output missing method %q\nmerged:\n%s", method, s)
		}
	}
}

func TestMergeTSInterfaceMembersReadonly(t *testing.T) {
	base := `interface Config {
  readonly host: string;
}`
	ours := `interface Config {
  readonly host: string;
  readonly port: number;
}`
	theirs := `interface Config {
  readonly host: string;
  timeout: number;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict")
	}

	s := string(merged)
	if !strings.Contains(s, "readonly port: number") {
		t.Errorf("merged output missing 'readonly port: number'\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "timeout: number") {
		t.Errorf("merged output missing 'timeout: number'\nmerged:\n%s", s)
	}
}

func TestMergeTSInterfaceMembersPreservesOrder(t *testing.T) {
	base := `interface Config {
  a: string;
  b: string;
  c: string;
}`
	ours := `interface Config {
  a: string;
  b: string;
  c: string;
  d: string;
}`
	theirs := `interface Config {
  a: string;
  b: string;
  c: string;
  e: string;
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "typescript")
	if conflict {
		t.Fatal("expected no conflict")
	}

	s := string(merged)
	aIdx := strings.Index(s, "a: string")
	bIdx := strings.Index(s, "b: string")
	cIdx := strings.Index(s, "c: string")
	dIdx := strings.Index(s, "d: string")
	eIdx := strings.Index(s, "e: string")

	if aIdx < 0 || bIdx < 0 || cIdx < 0 || dIdx < 0 || eIdx < 0 {
		t.Fatalf("missing members in merged output\nmerged:\n%s", s)
	}

	if !(aIdx < bIdx && bIdx < cIdx && cIdx < dIdx && dIdx < eIdx) {
		t.Errorf("members not in expected order (a < b < c < d < e)\nmerged:\n%s", s)
	}
}

// --- Edge cases ---

func TestMergeInterfaceMembersUnsupportedLanguage(t *testing.T) {
	_, conflict := MergeInterfaceMembers([]byte("{}"), []byte("{}"), []byte("{}"), "rust")
	if !conflict {
		t.Fatal("expected conflict for unsupported language")
	}
}

func TestMergeGoInterfaceMembersEmptyBase(t *testing.T) {
	base := `type Handler interface {
}`
	ours := `type Handler interface {
	Handle(req Request) Response
}`
	theirs := `type Handler interface {
	Close() error
}`

	merged, conflict := MergeInterfaceMembers([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for additions to empty interface")
	}

	s := string(merged)
	if !strings.Contains(s, "Handle(") {
		t.Errorf("merged output missing 'Handle('\nmerged:\n%s", s)
	}
	if !strings.Contains(s, "Close()") {
		t.Errorf("merged output missing 'Close()'\nmerged:\n%s", s)
	}
}
