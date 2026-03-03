package diff

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/diff3"
	"github.com/odvcencio/graft/pkg/entity"
)

// FormatReview produces a structural code review output for a file diff.
// Each entity change is tagged [ADDED], [MODIFIED], or [REMOVED] with the
// entity's display name and line range. Modified entities include an inline
// line-level diff of their body.
func FormatReview(d *FileDiff) string {
	if len(d.Changes) == 0 {
		return ""
	}

	// Filter to declaration-level entities only — interstitial/preamble changes
	// produce ugly raw identity keys and meaningless line ranges.
	var decls []EntityChange
	for _, c := range d.Changes {
		if isDeclChange(c) {
			decls = append(decls, c)
		}
	}
	if len(decls) == 0 {
		return ""
	}

	var added, modified, removed int
	for _, c := range decls {
		switch c.Type {
		case Added:
			added++
		case Modified:
			modified++
		case Removed:
			removed++
		}
	}
	total := len(decls)

	var b strings.Builder

	fmt.Fprintf(&b, "=== %s ===\n", d.Path)
	fmt.Fprintf(&b, "Summary: %d %s changed", total, pluralEntity(total))

	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", added))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", modified))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
	}
	b.WriteString("\n")

	for _, c := range decls {
		name := entityDisplayName(c)

		switch c.Type {
		case Added:
			fmt.Fprintf(&b, "\n[ADDED] %s (lines %d-%d)\n",
				name, c.After.StartLine, c.After.EndLine)

		case Modified:
			fmt.Fprintf(&b, "\n[MODIFIED] %s (lines %d-%d)\n",
				name, c.After.StartLine, c.After.EndLine)
			lines := diff3.LineDiff(c.Before.Body, c.After.Body)
			for _, dl := range lines {
				switch dl.Type {
				case diff3.Delete:
					fmt.Fprintf(&b, "  -%s\n", dl.Content)
				case diff3.Insert:
					fmt.Fprintf(&b, "  +%s\n", dl.Content)
				case diff3.Equal:
					fmt.Fprintf(&b, "   %s\n", dl.Content)
				}
			}

		case Removed:
			fmt.Fprintf(&b, "\n[REMOVED] %s (was lines %d-%d)\n",
				name, c.Before.StartLine, c.Before.EndLine)
		}
	}

	return b.String()
}

// pluralEntity returns "entity" or "entities" depending on count.
func pluralEntity(n int) string {
	if n == 1 {
		return "entity"
	}
	return "entities"
}

// isDeclChange returns true if the entity change is for a declaration-level entity.
func isDeclChange(c EntityChange) bool {
	if c.After != nil {
		return c.After.Kind == entity.KindDeclaration
	}
	if c.Before != nil {
		return c.Before.Kind == entity.KindDeclaration
	}
	return false
}
