package entity

// BuildEntityMap indexes entities by identity key. First occurrence wins to
// keep deterministic behavior if malformed input still contains duplicates.
func BuildEntityMap(el *EntityList) map[string]*Entity {
	m := make(map[string]*Entity, len(el.Entities))
	for i := range el.Entities {
		key := el.Entities[i].IdentityKey()
		if _, exists := m[key]; exists {
			continue
		}
		m[key] = &el.Entities[i]
	}
	return m
}

// OrderedIdentityKeys returns unique identity keys in first-seen order.
func OrderedIdentityKeys(el *EntityList) []string {
	keys := make([]string, 0, len(el.Entities))
	seen := make(map[string]bool, len(el.Entities))
	for i := range el.Entities {
		key := el.Entities[i].IdentityKey()
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}
