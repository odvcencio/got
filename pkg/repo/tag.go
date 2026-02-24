package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// CreateTag creates or updates a lightweight tag ref under refs/tags/.
func (r *Repo) CreateTag(name string, target object.Hash, force bool) error {
	name = strings.TrimSpace(name)
	if err := validateTagName(name); err != nil {
		return fmt.Errorf("create tag: %w", err)
	}
	if strings.TrimSpace(string(target)) == "" {
		return fmt.Errorf("create tag: target hash is required")
	}

	refName := "refs/tags/" + name
	if !force {
		if _, err := r.ResolveRef(refName); err == nil {
			return fmt.Errorf("create tag: tag %q already exists", name)
		}
	}
	if err := r.UpdateRef(refName, target); err != nil {
		return fmt.Errorf("create tag: %w", err)
	}
	return nil
}

// CreateAnnotatedTag creates or updates an annotated tag ref under refs/tags/.
// The ref points at a stored tag object, which in turn points at target.
func (r *Repo) CreateAnnotatedTag(name string, target object.Hash, tagger, message string, force bool) (object.Hash, error) {
	name = strings.TrimSpace(name)
	if err := validateTagName(name); err != nil {
		return "", fmt.Errorf("create annotated tag: %w", err)
	}
	if strings.TrimSpace(string(target)) == "" {
		return "", fmt.Errorf("create annotated tag: target hash is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("create annotated tag: message is required")
	}
	tagger = strings.TrimSpace(tagger)
	if tagger == "" {
		tagger = "unknown"
	}

	targetType, _, err := r.Store.Read(target)
	if err != nil {
		return "", fmt.Errorf("create annotated tag: read target %s: %w", target, err)
	}

	refName := "refs/tags/" + name
	if !force {
		if _, err := r.ResolveRef(refName); err == nil {
			return "", fmt.Errorf("create annotated tag: tag %q already exists", name)
		}
	}

	now := time.Now()
	payload := fmt.Sprintf(
		"object %s\n"+
			"type %s\n"+
			"tag %s\n"+
			"tagger %s %d %s\n\n"+
			"%s\n",
		target,
		targetType,
		name,
		tagger,
		now.Unix(),
		formatTimezoneOffset(now),
		message,
	)
	tagHash, err := r.Store.WriteTag(&object.TagObj{
		TargetHash: target,
		Data:       []byte(payload),
	})
	if err != nil {
		return "", fmt.Errorf("create annotated tag: write tag object: %w", err)
	}

	if err := r.UpdateRef(refName, tagHash); err != nil {
		return "", fmt.Errorf("create annotated tag: %w", err)
	}
	return tagHash, nil
}

// DeleteTag removes a tag ref from refs/tags/.
func (r *Repo) DeleteTag(name string) error {
	name = strings.TrimSpace(name)
	if err := validateTagName(name); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}

	refPath := filepath.Join(r.GotDir, "refs", "tags", filepath.FromSlash(name))
	if err := os.Remove(refPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("delete tag: tag %q does not exist", name)
		}
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// ResolveTag resolves a tag name under refs/tags/.
func (r *Repo) ResolveTag(name string) (object.Hash, error) {
	name = strings.TrimSpace(name)
	if err := validateTagName(name); err != nil {
		return "", fmt.Errorf("resolve tag: %w", err)
	}
	return r.ResolveRef("refs/tags/" + name)
}

// ListTags lists tag names sorted alphabetically.
func (r *Repo) ListTags() ([]string, error) {
	refs, err := r.ListRefs("tags")
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	names := make([]string, 0, len(refs))
	for full := range refs {
		name := strings.TrimPrefix(full, "tags/")
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ListTagsWithHashes returns tag name -> target hash.
func (r *Repo) ListTagsWithHashes() (map[string]object.Hash, error) {
	refs, err := r.ListRefs("tags")
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	out := make(map[string]object.Hash, len(refs))
	for full, hash := range refs {
		name := strings.TrimPrefix(full, "tags/")
		out[name] = hash
	}
	return out, nil
}

func validateTagName(name string) error {
	if name == "" {
		return fmt.Errorf("tag name is required")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("invalid tag name %q", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid tag name %q", name)
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("invalid tag name %q", name)
	}
	return nil
}

func formatTimezoneOffset(t time.Time) string {
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
}
