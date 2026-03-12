package gotreesitter

// WalkAction controls the tree walk behavior.
type WalkAction int

const (
	// WalkContinue continues the walk to children and siblings.
	WalkContinue WalkAction = iota
	// WalkSkipChildren skips the current node's children but continues to siblings.
	WalkSkipChildren
	// WalkStop terminates the walk entirely.
	WalkStop
)

// Walk performs a depth-first traversal of the syntax tree rooted at node.
// The callback receives each node and its depth (0 for the starting node).
// Return WalkSkipChildren to skip a node's children, or WalkStop to end early.
func Walk(node *Node, fn func(node *Node, depth int) WalkAction) {
	if node == nil {
		return
	}

	type entry struct {
		node  *Node
		depth int
	}

	stack := []entry{{node: node, depth: 0}}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		action := fn(e.node, e.depth)
		if action == WalkStop {
			return
		}
		if action == WalkSkipChildren {
			continue
		}

		children := e.node.Children()
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, entry{node: children[i], depth: e.depth + 1})
		}
	}
}
