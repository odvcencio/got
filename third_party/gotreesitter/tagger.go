package gotreesitter

// Tag represents a tagged symbol in source code, extracted by a Tagger.
// Kind follows tree-sitter convention: "definition.function", "reference.call", etc.
// Name is the captured symbol text (e.g., the function name).
type Tag struct {
	Kind      string // e.g. "definition.function", "reference.call"
	Name      string // the captured symbol text
	Range     Range  // full span of the tagged node
	NameRange Range  // span of the @name capture
}

// Tagger extracts symbol definitions and references from source code using
// tree-sitter tags queries. It is the tagging counterpart to Highlighter.
//
// Tags queries use a convention where captures follow the pattern:
//   - @name captures the symbol name (e.g., function identifier)
//   - @definition.X or @reference.X captures the kind
//
// Example query:
//
//	(function_declaration name: (identifier) @name) @definition.function
//	(call_expression function: (identifier) @name) @reference.call
type Tagger struct {
	parser             *Parser
	query              *Query
	lang               *Language
	tokenSourceFactory func(source []byte) TokenSource
}

// TaggerOption configures a Tagger.
type TaggerOption func(*Tagger)

// WithTaggerTokenSourceFactory sets a factory function that creates a TokenSource
// for each Tag call.
func WithTaggerTokenSourceFactory(factory func(source []byte) TokenSource) TaggerOption {
	return func(tg *Tagger) {
		tg.tokenSourceFactory = factory
	}
}

// NewTagger creates a Tagger for the given language and tags query.
func NewTagger(lang *Language, tagsQuery string, opts ...TaggerOption) (*Tagger, error) {
	q, err := NewQuery(tagsQuery, lang)
	if err != nil {
		return nil, err
	}

	tg := &Tagger{
		parser: NewParser(lang),
		query:  q,
		lang:   lang,
	}
	for _, opt := range opts {
		opt(tg)
	}
	return tg, nil
}

// Tag parses source and returns all tags.
func (tg *Tagger) Tag(source []byte) []Tag {
	if len(source) == 0 {
		return nil
	}

	tree := tg.parse(source, nil)
	if tree.RootNode() == nil {
		return nil
	}
	defer tree.Release()

	return tg.tagTree(tree)
}

// TagTree extracts tags from an already-parsed tree.
func (tg *Tagger) TagTree(tree *Tree) []Tag {
	if tree == nil || tree.RootNode() == nil {
		return nil
	}
	return tg.tagTree(tree)
}

// TagIncremental re-tags source after edits to oldTree.
// Returns the tags and the new tree for subsequent incremental calls.
func (tg *Tagger) TagIncremental(source []byte, oldTree *Tree) ([]Tag, *Tree) {
	if len(source) == 0 {
		return nil, NewTree(nil, source, tg.lang)
	}

	tree := tg.parse(source, oldTree)
	if tree.RootNode() == nil {
		return nil, tree
	}

	return tg.tagTree(tree), tree
}

func (tg *Tagger) parse(source []byte, oldTree *Tree) *Tree {
	return dispatchParse(tg.parser, source, oldTree, tg.tokenSourceFactory, tg.lang)
}

func (tg *Tagger) tagTree(tree *Tree) []Tag {
	matches := tg.query.Execute(tree)
	if len(matches) == 0 {
		return nil
	}

	var tags []Tag
	for _, m := range matches {
		tag := tg.extractTag(m, tree.Source())
		if tag.Kind != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func (tg *Tagger) extractTag(m QueryMatch, source []byte) Tag {
	var tag Tag
	for _, c := range m.Captures {
		switch {
		case c.Name == "name":
			tag.Name = c.Node.Text(source)
			tag.NameRange = c.Node.Range()
		case len(c.Name) > 11 && c.Name[:11] == "definition." ||
			len(c.Name) > 10 && c.Name[:10] == "reference.":
			tag.Kind = c.Name
			tag.Range = c.Node.Range()
		}
	}
	if tag.Kind != "" && tag.Name == "" {
		tag.Name = string(source[tag.Range.StartByte:tag.Range.EndByte])
		tag.NameRange = tag.Range
	}
	return tag
}
