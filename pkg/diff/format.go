package diff

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/diff3"
	"github.com/odvcencio/graft/pkg/entity"
)

// FormatEntityDiff produces a human-readable entity-level summary of changes.
//
// Output format:
//
//	path:
//	  + func Name     (added)
//	  ~ func Name     (modified)
//	  - func Name     (removed)
func FormatEntityDiff(d *FileDiff) string {
	if len(d.Changes) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", d.Path)

	for _, c := range d.Changes {
		var marker string
		var label string
		switch c.Type {
		case Added:
			marker = "+"
			label = "added"
		case Removed:
			marker = "-"
			label = "removed"
		case Modified:
			marker = "~"
			label = "modified"
		}

		name := entityDisplayName(c)
		fmt.Fprintf(&b, "  %s %s     (%s)\n", marker, name, label)
	}

	return b.String()
}

// FormatLineDiff produces a unified-diff-style output showing line-level
// changes within modified entities. Only Modified changes produce output;
// Added/Removed entities are shown in full.
//
// Output format for Modified entities:
//
//	--- a/path::Name
//	+++ b/path::Name
//	-    old line
//	+    new line
func FormatLineDiff(d *FileDiff) string {
	if len(d.Changes) == 0 {
		return ""
	}

	var b strings.Builder

	for _, c := range d.Changes {
		switch c.Type {
		case Modified:
			name := entityDisplayName(c)
			fmt.Fprintf(&b, "--- a/%s::%s\n", d.Path, name)
			fmt.Fprintf(&b, "+++ b/%s::%s\n", d.Path, name)

			lines := diff3.LineDiff(c.Before.Body, c.After.Body)
			for _, dl := range lines {
				switch dl.Type {
				case diff3.Delete:
					fmt.Fprintf(&b, "-%s\n", dl.Content)
				case diff3.Insert:
					fmt.Fprintf(&b, "+%s\n", dl.Content)
				case diff3.Equal:
					fmt.Fprintf(&b, " %s\n", dl.Content)
				}
			}

		case Added:
			name := entityDisplayName(c)
			fmt.Fprintf(&b, "+++ b/%s::%s\n", d.Path, name)
			bodyLines := strings.Split(strings.TrimRight(string(c.After.Body), "\n"), "\n")
			for _, l := range bodyLines {
				fmt.Fprintf(&b, "+%s\n", l)
			}

		case Removed:
			name := entityDisplayName(c)
			fmt.Fprintf(&b, "--- a/%s::%s\n", d.Path, name)
			bodyLines := strings.Split(strings.TrimRight(string(c.Before.Body), "\n"), "\n")
			for _, l := range bodyLines {
				fmt.Fprintf(&b, "-%s\n", l)
			}
		}
	}

	return b.String()
}

// entityDisplayName returns a human-readable label for the changed entity.
// For declarations it delegates to entity.EntityDisplayName; for other kinds
// it uses the identity key from the change.
func entityDisplayName(c EntityChange) string {
	var e *entity.Entity
	if c.After != nil {
		e = c.After
	} else {
		e = c.Before
	}

	if e.Kind == entity.KindDeclaration {
		return entity.EntityDisplayName(e)
	}

	return c.Key
}
