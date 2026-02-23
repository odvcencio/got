package entity

// Reconstruct concatenates entity bodies in order to reproduce the original source.
// The Extract function guarantees byte coverage, so this reproduces the original
// source byte-for-byte when entities are unmodified.
func Reconstruct(el *EntityList) []byte {
	if el == nil || len(el.Entities) == 0 {
		return nil
	}

	// Pre-compute total size to avoid repeated allocations.
	total := 0
	for i := range el.Entities {
		total += len(el.Entities[i].Body)
	}

	buf := make([]byte, 0, total)
	for i := range el.Entities {
		buf = append(buf, el.Entities[i].Body...)
	}
	return buf
}
