package browser

import (
	"encoding/json"
	"fmt"

	"github.com/cnjack/jcode/internal/uitree"
)

// axNode mirrors the CDP Accessibility.AXNode shape (the fields we use).
type axNode struct {
	NodeID           string   `json:"nodeId"`
	Ignored          bool     `json:"ignored"`
	Role             *axValue `json:"role"`
	Name             *axValue `json:"name"`
	Value            *axValue `json:"value"`
	Properties       []axProp `json:"properties"`
	ChildIDs         []string `json:"childIds"`
	ParentID         string   `json:"parentId"`
	BackendDOMNodeID int64    `json:"backendDOMNodeId"`
}

type axValue struct {
	Value any `json:"value"`
}

func (v *axValue) str() string {
	if v == nil || v.Value == nil {
		return ""
	}
	switch t := v.Value.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

type axProp struct {
	Name  string   `json:"name"`
	Value *axValue `json:"value"`
}

// Snapshot is one serialized page state. The uid minting, role vocabulary and
// elision live in internal/uitree, shared with computer-use so the two cannot
// drift; see internal-doc/computer-use-design.md §3.1.
type Snapshot = uitree.Snapshot

const defaultMaxLines = uitree.DefaultMaxLines

// buildSnapshot adapts a CDP AX tree into the shared uitree form and serializes
// it. The CDP role vocabulary is already lowercase-ish and matches
// uitree.InteractiveRoles directly, so no role mapping is needed here.
//
// known is the previous snapshot's Ref→uid binding and uidBase the session's
// monotonic counter; see uitree.Snapshot for why a uid must name an element
// rather than a position.
func buildSnapshot(nodes []axNode, filter string, gen int, maxLines int, known map[int64]string, uidBase int) *Snapshot {
	generic := make([]uitree.Node, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		states := make([]uitree.State, 0, len(n.Properties))
		for _, p := range n.Properties {
			states = append(states, uitree.State{Name: p.Name, Value: p.Value.str()})
		}
		generic = append(generic, uitree.Node{
			ID:       n.NodeID,
			Role:     n.Role.str(),
			Name:     n.Name.str(),
			Value:    n.Value.str(),
			States:   states,
			ChildIDs: n.ChildIDs,
			Ref:      n.BackendDOMNodeID,
			Ignored:  n.Ignored,
		})
	}
	return uitree.Build(generic, filter, gen, maxLines, known, uidBase)
}

// truncate cuts on a rune boundary. Retained as the package-local spelling of
// uitree.Truncate.
func truncate(s string, n int) string { return uitree.Truncate(s, n) }

// parseAXTree decodes an Accessibility.getFullAXTree result.
func parseAXTree(raw json.RawMessage) ([]axNode, error) {
	var out struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse AX tree: %w", err)
	}
	return out.Nodes, nil
}
