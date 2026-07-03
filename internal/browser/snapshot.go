package browser

import (
	"encoding/json"
	"fmt"
	"strings"
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

// interactiveRoles are AX roles that receive a uid and can be targeted by
// browser_act. Aligned with what Codex/Claude snapshots mark as actionable.
var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true,
	"checkbox": true, "radio": true, "combobox": true, "listbox": true,
	"option": true, "menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
	"tab": true, "switch": true, "slider": true, "spinbutton": true,
	"textfield": true, "textarea": true, "MenuListPopup": true,
}

// contextRoles are shown without a uid to give the model structure.
var contextRoles = map[string]bool{
	"heading": true, "img": true, "image": true, "alert": true, "dialog": true,
	"status": true, "tabpanel": true, "cell": true, "columnheader": true,
	"rowheader": true, "listitem": true,
}

// Snapshot is one serialized page state. UIDs are only valid for the
// generation they were minted in; actions verify this to reject stale refs.
type Snapshot struct {
	Text string
	UIDs map[string]int64 // uid → backendDOMNodeId
	Gen  int
}

const defaultMaxLines = 400

// buildSnapshot serializes an AX tree into a compact uid-annotated text form.
// filter: "interactive" (default) emits interactive + context nodes,
// "all" additionally emits static text.
func buildSnapshot(nodes []axNode, filter string, gen int, maxLines int) *Snapshot {
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	byID := make(map[string]*axNode, len(nodes))
	hasParent := make(map[string]bool)
	for i := range nodes {
		byID[nodes[i].NodeID] = &nodes[i]
		for _, c := range nodes[i].ChildIDs {
			hasParent[c] = true
		}
	}

	var roots []*axNode
	for i := range nodes {
		if !hasParent[nodes[i].NodeID] {
			roots = append(roots, &nodes[i])
		}
	}

	snap := &Snapshot{UIDs: make(map[string]int64), Gen: gen}
	var lines []string
	uidSeq := 0
	elided := 0
	interactiveCount := 0

	var walk func(n *axNode, depth int)
	walk = func(n *axNode, depth int) {
		if n == nil {
			return
		}
		if !n.Ignored {
			role := n.Role.str()
			name := strings.TrimSpace(n.Name.str())
			line := ""
			switch {
			case interactiveRoles[role] && n.BackendDOMNodeID != 0:
				uidSeq++
				uid := fmt.Sprintf("e%d", uidSeq)
				snap.UIDs[uid] = n.BackendDOMNodeID
				interactiveCount++
				line = fmt.Sprintf("[%s] %s %q%s", uid, role, truncate(name, 120), axStates(n))
			case contextRoles[role] && name != "":
				line = fmt.Sprintf("- %s %q", role, truncate(name, 120))
			case filter == "all" && (role == "StaticText" || role == "text") && name != "":
				line = fmt.Sprintf("  %s", truncate(name, 160))
			}
			if line != "" {
				if len(lines) < maxLines {
					lines = append(lines, line)
				} else {
					elided++
				}
			}
		}
		for _, cid := range n.ChildIDs {
			walk(byID[cid], depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}

	if elided > 0 {
		lines = append(lines, fmt.Sprintf("… %d more nodes elided (interactive=%d, filter=%s)", elided, interactiveCount, filterOrDefault(filter)))
	}
	snap.Text = strings.Join(lines, "\n")
	return snap
}

func filterOrDefault(f string) string {
	if f == "" {
		return "interactive"
	}
	return f
}

// axStates renders the interesting boolean/value states of a node.
func axStates(n *axNode) string {
	var states []string
	if v := strings.TrimSpace(n.Value.str()); v != "" {
		states = append(states, fmt.Sprintf("value=%q", truncate(v, 80)))
	}
	for _, p := range n.Properties {
		switch p.Name {
		case "disabled", "focused", "expanded", "selected", "required", "readonly", "modal":
			if p.Value.str() == "true" {
				states = append(states, p.Name)
			}
		case "checked", "pressed":
			if s := p.Value.str(); s != "" && s != "false" {
				if s == "true" {
					states = append(states, p.Name)
				} else {
					states = append(states, p.Name+"="+s)
				}
			}
		case "invalid":
			if s := p.Value.str(); s != "" && s != "false" {
				states = append(states, "invalid")
			}
		}
	}
	if len(states) == 0 {
		return ""
	}
	return " (" + strings.Join(states, ", ") + ")"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

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
