package browser

import (
	"fmt"
	"strings"
)

// AXValue represents a typed value in the CDP accessibility tree response.
// CDP returns role, name, and value as objects like {"type": "role", "value": "button"}.
type AXValue struct {
	Value string `json:"value"`
}

// AXNode represents a single node in the CDP Accessibility.getFullAXTree response.
// The CDP response is a flat array; parent-child relationships are expressed via
// NodeID and ParentID fields.
type AXNode struct {
	NodeID           string   `json:"nodeId"`
	ParentID         string   `json:"parentId"`
	Role             AXValue  `json:"role"`
	Name             AXValue  `json:"name"`
	Value            *AXValue `json:"value,omitempty"`
	Children         []AXNode `json:"children,omitempty"`
	BackendDOMNodeId int      `json:"backendDOMNodeId,omitempty"`
}

// filteredRoles is the set of roles that carry no semantic signal and should be
// excluded from the formatted output.
var filteredRoles = map[string]bool{
	"none":          true,
	"generic":       true,
	"InlineTextBox": true,
}

// FormatAccessibilityTree converts a flat CDP AX node array into a human-readable
// tree string. It builds the parent-child graph, walks depth-first from root nodes,
// filters semantic noise, and formats each surviving node as `[role] "name"`.
func FormatAccessibilityTree(nodes []AXNode) string {
	if len(nodes) == 0 {
		return ""
	}

	// Index nodes by ID and collect all IDs that appear as a parent.
	byID := make(map[string]*AXNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].NodeID] = &nodes[i]
	}

	// Build children map: parentID -> ordered child IDs.
	children := make(map[string][]string, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		if n.ParentID != "" {
			if _, parentExists := byID[n.ParentID]; parentExists {
				children[n.ParentID] = append(children[n.ParentID], n.NodeID)
			}
		}
	}

	// Identify root nodes: no parent, or parent not present in the node set.
	var roots []string
	for i := range nodes {
		n := &nodes[i]
		if n.ParentID == "" {
			roots = append(roots, n.NodeID)
			continue
		}
		if _, ok := byID[n.ParentID]; !ok {
			roots = append(roots, n.NodeID)
		}
	}

	var sb strings.Builder
	const maxDepth = 10

	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if depth > maxDepth {
			return
		}
		n, ok := byID[id]
		if !ok {
			return
		}

		role := n.Role.Value
		name := n.Name.Value
		skip := filteredRoles[role] || role == ""

		if !skip {
			// Emit this node only if it has a name, or if we'd emit it as
			// a structural container (checked after recursing children).
			indent := strings.Repeat("  ", depth)
			line := fmt.Sprintf("%s[%s] %q", indent, role, name)
			if n.Value != nil && n.Value.Value != "" {
				line += fmt.Sprintf(" = %q", n.Value.Value)
			}
			sb.WriteString(line)
			sb.WriteByte('\n')
		}

		for _, childID := range children[id] {
			walk(childID, depth+1)
		}
	}

	for _, rootID := range roots {
		walk(rootID, 0)
	}

	return sb.String()
}
