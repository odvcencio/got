package gotreesitter

import "sort"

// cursorFrame tracks a node and the child index within its parent.
// childIndex is -1 for the cursor root (no parent context).
type cursorFrame struct {
	node       *Node
	childIndex int
}

// TreeCursor provides stateful, O(1) tree navigation.
// It maintains a stack of (node, childIndex) frames enabling efficient
// parent, child, and sibling movement without scanning.
//
// The cursor holds pointers to Nodes. If the underlying Tree is released,
// edited, or replaced via incremental reparse, the cursor should be recreated.
type TreeCursor struct {
	stack []cursorFrame
	tree  *Tree
}

// NewTreeCursor creates a cursor starting at the given node.
// The optional tree reference enables field name resolution and text extraction.
func NewTreeCursor(node *Node, tree *Tree) *TreeCursor {
	return &TreeCursor{
		stack: []cursorFrame{{node: node, childIndex: -1}},
		tree:  tree,
	}
}

// NewTreeCursorFromTree creates a cursor starting at the tree's root node.
func NewTreeCursorFromTree(tree *Tree) *TreeCursor {
	if tree == nil {
		return NewTreeCursor(nil, nil)
	}
	return NewTreeCursor(tree.RootNode(), tree)
}

func (c *TreeCursor) ensureStack() {
	if len(c.stack) == 0 {
		c.stack = []cursorFrame{{node: nil, childIndex: -1}}
	}
}

// CurrentNode returns the node the cursor is currently pointing to.
func (c *TreeCursor) CurrentNode() *Node {
	if len(c.stack) == 0 {
		return nil
	}
	return c.stack[len(c.stack)-1].node
}

// Depth returns the cursor's current depth (0 at the root).
func (c *TreeCursor) Depth() int {
	if len(c.stack) == 0 {
		return 0
	}
	return len(c.stack) - 1
}

// GotoFirstChild moves the cursor to the first child of the current node.
// Returns false if the current node has no children.
func (c *TreeCursor) GotoFirstChild() bool {
	node := c.CurrentNode()
	if node == nil || len(node.children) == 0 {
		return false
	}
	c.stack = append(c.stack, cursorFrame{node: node.children[0], childIndex: 0})
	return true
}

// GotoLastChild moves the cursor to the last child of the current node.
// Returns false if the current node has no children.
func (c *TreeCursor) GotoLastChild() bool {
	node := c.CurrentNode()
	if node == nil {
		return false
	}
	n := len(node.children)
	if n == 0 {
		return false
	}
	c.stack = append(c.stack, cursorFrame{node: node.children[n-1], childIndex: n - 1})
	return true
}

// GotoNextSibling moves the cursor to the next sibling.
// Returns false if the cursor is at the root or the last sibling.
func (c *TreeCursor) GotoNextSibling() bool {
	if len(c.stack) < 2 {
		return false
	}
	frame := &c.stack[len(c.stack)-1]
	parentNode := c.stack[len(c.stack)-2].node
	if parentNode == nil {
		return false
	}
	next := frame.childIndex + 1
	if next >= len(parentNode.children) {
		return false
	}
	frame.childIndex = next
	frame.node = parentNode.children[next]
	return true
}

// GotoPrevSibling moves the cursor to the previous sibling.
// Returns false if the cursor is at the root or the first sibling.
func (c *TreeCursor) GotoPrevSibling() bool {
	if len(c.stack) < 2 {
		return false
	}
	frame := &c.stack[len(c.stack)-1]
	parentNode := c.stack[len(c.stack)-2].node
	if parentNode == nil {
		return false
	}
	prev := frame.childIndex - 1
	if prev < 0 {
		return false
	}
	frame.childIndex = prev
	frame.node = parentNode.children[prev]
	return true
}

// GotoParent moves the cursor to the parent of the current node.
// Returns false if the cursor is at the root.
func (c *TreeCursor) GotoParent() bool {
	if len(c.stack) < 2 {
		return false
	}
	c.stack = c.stack[:len(c.stack)-1]
	return true
}

// CurrentFieldID returns the field ID of the current node within its parent.
// Returns 0 if the cursor is at the root or the node has no field assignment.
func (c *TreeCursor) CurrentFieldID() FieldID {
	if len(c.stack) < 2 {
		return 0
	}
	frame := c.stack[len(c.stack)-1]
	parentNode := c.stack[len(c.stack)-2].node
	if parentNode == nil {
		return 0
	}
	if frame.childIndex < len(parentNode.fieldIDs) {
		return parentNode.fieldIDs[frame.childIndex]
	}
	return 0
}

// CurrentFieldName returns the field name of the current node within its parent.
// Returns "" if no tree is associated, the cursor is at the root, or
// the node has no field assignment.
func (c *TreeCursor) CurrentFieldName() string {
	fid := c.CurrentFieldID()
	if fid == 0 || c.tree == nil {
		return ""
	}
	lang := c.tree.Language()
	if lang == nil || int(fid) >= len(lang.FieldNames) {
		return ""
	}
	return lang.FieldNames[fid]
}

// GotoChildByFieldID moves the cursor to the first child with the given field ID.
// Returns false if no child has that field.
func (c *TreeCursor) GotoChildByFieldID(fid FieldID) bool {
	if fid == 0 {
		// Field ID 0 is a sentinel meaning "no field assignment".
		return false
	}
	node := c.CurrentNode()
	if node == nil {
		return false
	}
	for i, id := range node.fieldIDs {
		if id == fid && i < len(node.children) {
			c.stack = append(c.stack, cursorFrame{node: node.children[i], childIndex: i})
			return true
		}
	}
	return false
}

// GotoChildByFieldName moves the cursor to the first child with the given field name.
// Returns false if the tree has no language, the field name is unknown, or
// no child has that field.
func (c *TreeCursor) GotoChildByFieldName(name string) bool {
	if c.tree == nil {
		return false
	}
	lang := c.tree.Language()
	if lang == nil {
		return false
	}
	fid, ok := lang.FieldByName(name)
	if !ok || fid == 0 {
		return false
	}
	return c.GotoChildByFieldID(fid)
}

// GotoFirstNamedChild moves the cursor to the first named child of the
// current node, skipping anonymous nodes. Returns false if no named child exists.
func (c *TreeCursor) GotoFirstNamedChild() bool {
	node := c.CurrentNode()
	if node == nil {
		return false
	}
	for i, child := range node.children {
		if child.isNamed {
			c.stack = append(c.stack, cursorFrame{node: child, childIndex: i})
			return true
		}
	}
	return false
}

// GotoLastNamedChild moves the cursor to the last named child of the
// current node, skipping anonymous nodes. Returns false if no named child exists.
func (c *TreeCursor) GotoLastNamedChild() bool {
	node := c.CurrentNode()
	if node == nil {
		return false
	}
	for i := len(node.children) - 1; i >= 0; i-- {
		if node.children[i].isNamed {
			c.stack = append(c.stack, cursorFrame{node: node.children[i], childIndex: i})
			return true
		}
	}
	return false
}

// GotoNextNamedSibling moves the cursor to the next named sibling,
// skipping anonymous nodes. Returns false if no named sibling follows.
func (c *TreeCursor) GotoNextNamedSibling() bool {
	if len(c.stack) < 2 {
		return false
	}
	frame := &c.stack[len(c.stack)-1]
	parentNode := c.stack[len(c.stack)-2].node
	if parentNode == nil {
		return false
	}
	for i := frame.childIndex + 1; i < len(parentNode.children); i++ {
		if parentNode.children[i].isNamed {
			frame.childIndex = i
			frame.node = parentNode.children[i]
			return true
		}
	}
	return false
}

// GotoPrevNamedSibling moves the cursor to the previous named sibling,
// skipping anonymous nodes. Returns false if no named sibling precedes.
func (c *TreeCursor) GotoPrevNamedSibling() bool {
	if len(c.stack) < 2 {
		return false
	}
	frame := &c.stack[len(c.stack)-1]
	parentNode := c.stack[len(c.stack)-2].node
	if parentNode == nil {
		return false
	}
	for i := frame.childIndex - 1; i >= 0; i-- {
		if parentNode.children[i].isNamed {
			frame.childIndex = i
			frame.node = parentNode.children[i]
			return true
		}
	}
	return false
}

func pointGreaterThan(a, b Point) bool {
	if a.Row != b.Row {
		return a.Row > b.Row
	}
	return a.Column > b.Column
}

// GotoFirstChildForByte moves the cursor to the first child whose byte range
// contains targetByte (i.e., first child where endByte > targetByte).
// Returns the child index, or -1 when no child contains the byte.
func (c *TreeCursor) GotoFirstChildForByte(targetByte uint32) int64 {
	node := c.CurrentNode()
	if node == nil || len(node.children) == 0 {
		return -1
	}
	i := sort.Search(len(node.children), func(i int) bool {
		return node.children[i].endByte > targetByte
	})
	if i >= len(node.children) {
		return -1
	}
	c.stack = append(c.stack, cursorFrame{node: node.children[i], childIndex: i})
	return int64(i)
}

// GotoFirstChildForPoint moves the cursor to the first child whose point range
// contains targetPoint (i.e., first child where endPoint > targetPoint).
// Returns the child index, or -1 when no child contains the point.
func (c *TreeCursor) GotoFirstChildForPoint(targetPoint Point) int64 {
	node := c.CurrentNode()
	if node == nil || len(node.children) == 0 {
		return -1
	}
	i := sort.Search(len(node.children), func(i int) bool {
		return pointGreaterThan(node.children[i].endPoint, targetPoint)
	})
	if i >= len(node.children) {
		return -1
	}
	c.stack = append(c.stack, cursorFrame{node: node.children[i], childIndex: i})
	return int64(i)
}

// Reset resets the cursor to a new root node, clearing the navigation stack.
func (c *TreeCursor) Reset(node *Node) {
	c.ensureStack()
	c.stack = c.stack[:1]
	c.stack[0] = cursorFrame{node: node, childIndex: -1}
}

// ResetTree resets the cursor to the root of a new tree.
func (c *TreeCursor) ResetTree(tree *Tree) {
	c.tree = tree
	if tree == nil {
		c.Reset(nil)
		return
	}
	c.Reset(tree.RootNode())
}

// Copy returns an independent copy of the cursor. The copy shares the same
// tree reference but has its own navigation stack.
func (c *TreeCursor) Copy() *TreeCursor {
	newStack := make([]cursorFrame, len(c.stack))
	copy(newStack, c.stack)
	return &TreeCursor{
		stack: newStack,
		tree:  c.tree,
	}
}

// CurrentNodeType returns the type name of the current node.
// Requires a tree with a language to be associated.
func (c *TreeCursor) CurrentNodeType() string {
	if c.tree == nil {
		return ""
	}
	node := c.CurrentNode()
	if node == nil {
		return ""
	}
	lang := c.tree.Language()
	if lang == nil {
		return ""
	}
	return node.Type(lang)
}

// CurrentNodeText returns the source text of the current node.
// Requires a tree with source to be associated.
func (c *TreeCursor) CurrentNodeText() string {
	if c.tree == nil {
		return ""
	}
	node := c.CurrentNode()
	if node == nil {
		return ""
	}
	return node.Text(c.tree.Source())
}

// CurrentNodeIsNamed returns whether the current node is a named node.
func (c *TreeCursor) CurrentNodeIsNamed() bool {
	node := c.CurrentNode()
	if node == nil {
		return false
	}
	return node.IsNamed()
}
