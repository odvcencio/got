package merge

import (
	"strings"
	"testing"
)

func TestMergeImportsUnion(t *testing.T) {
	base := `import "fmt"`
	ours := "import (\n\t\"fmt\"\n\t\"os\"\n)"
	theirs := "import (\n\t\"fmt\"\n\t\"net/http\"\n)"

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent import additions")
	}

	// Should contain all three: fmt, os, net/http
	s := string(merged)
	for _, imp := range []string{"fmt", "os", "net/http"} {
		if !strings.Contains(s, imp) {
			t.Errorf("merged output missing import %q", imp)
		}
	}
}

func TestMergeImportsBothRemove(t *testing.T) {
	base := "import (\n\t\"fmt\"\n\t\"os\"\n)"
	ours := `import "fmt"`
	theirs := `import "fmt"`

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict when both remove same import")
	}

	s := string(merged)
	if strings.Contains(s, "os") {
		t.Error("merged output should not contain removed import 'os'")
	}
	if !strings.Contains(s, "fmt") {
		t.Error("merged output should contain 'fmt'")
	}
}
