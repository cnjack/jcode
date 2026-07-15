// Package uitree renders an accessibility tree into the compact, uid-annotated
// text form that the agent reads and acts on.
//
// It is shared by browser-use (CDP Accessibility.getFullAXTree) and computer-use
// (macOS AXUIElement). Both produce a tree of role/name/value/state nodes, so
// both get the same on-screen vocabulary: an agent that has learned to read a
// browser snapshot can read a native app snapshot with no new concepts.
//
// Callers adapt their backend's node shape into []Node and call Build. The uid
// minting, generation stamping, role vocabulary, state rendering and elision all
// live here so the two consumers cannot drift.
//
// See internal-doc/computer-use-design.md §3.1.
package uitree

import (
	"fmt"
	"strings"
)

// State is one raw accessibility state pair as reported by a backend.
// Value is "" for a bare flag. Build decides which states are interesting.
type State struct {
	Name  string
	Value string
}

// Node is a backend-neutral accessibility node.
//
// Ref is the backend's own handle for the element (a CDP backendDOMNodeId, an
// AX element index); Build only stores it, never interprets it. A node is only
// eligible for a uid when Ref != 0, since a uid the backend cannot resolve back
// to an element is worse than no uid at all.
type Node struct {
	ID       string
	Role     string
	Name     string
	Value    string
	States   []State
	ChildIDs []string
	Ref      int64
	Ignored  bool
}

// Snapshot is one serialized tree state.
//
// A uid names an *element*, not a position. It is bound to a node's Ref for as
// long as that element keeps appearing, and is **never reused** once the element
// is gone. That is what makes "absent from the latest snapshot" mean "stale".
//
// This is load-bearing, and the obvious implementation gets it wrong. If each
// snapshot restarts its uid sequence at e1, a uid is silently *rebound* rather
// than invalidated: the model reads `[e1] button "New Note"`, the tree changes,
// the next snapshot mints `[e1] button "Delete All Notes"`, and an action
// carrying the remembered `e1` resolves cleanly — to the wrong button. Presence
// in the latest map is then a perfect disguise for staleness, and the check that
// is supposed to prevent a misdirected click is the thing that permits it.
//
// Binding uid↔Ref fixes it in both directions at once: a surviving element keeps
// its uid (so a remembered uid stays valid, because it really is the same
// element, and so consecutive snapshots diff to nothing), while a departed
// element's uid is retired forever (so a remembered uid for it is simply absent,
// which resolveUID already rejects).
//
// Refs and NextUID carry the binding forward to the next Build on the session.
type Snapshot struct {
	Text string
	UIDs map[string]int64 // uid → Node.Ref
	Gen  int
	// Refs is the reverse binding (Ref → uid) for every element in this
	// snapshot. Pass it back as `known` on the next Build for the same surface.
	Refs map[int64]string
	// NextUID is the first uid number this snapshot did not use.
	NextUID int
}

// InteractiveRoles are roles that receive a uid and can be targeted by an
// action. Aligned with what Codex/Claude snapshots mark as actionable.
//
// Both the CDP and the macOS AX vocabularies are normalized into these names by
// their adapters (AXButton → button, AXTextField → textbox, …).
var InteractiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true,
	"checkbox": true, "radio": true, "combobox": true, "listbox": true,
	"option": true, "menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
	"tab": true, "switch": true, "slider": true, "spinbutton": true,
	"textfield": true, "textarea": true, "MenuListPopup": true,
	// Native-only additions. Harmless for the browser adapter, which never
	// emits them.
	"menubutton": true, "popupbutton": true, "incrementor": true,
	"disclosuretriangle": true, "row": true, "colorwell": true,
}

// ContextRoles are shown without a uid to give the model structure.
var ContextRoles = map[string]bool{
	"heading": true, "img": true, "image": true, "alert": true, "dialog": true,
	"status": true, "tabpanel": true, "cell": true, "columnheader": true,
	"rowheader": true, "listitem": true,
	// Native-only additions.
	"window": true, "sheet": true, "group": true, "toolbar": true,
	"statictext": true,
}

// DefaultMaxLines bounds a snapshot so one enormous window cannot blow the
// context window.
const DefaultMaxLines = 400

// flagStates render as a bare name when true.
var flagStates = map[string]bool{
	"disabled": true, "focused": true, "expanded": true, "selected": true,
	"required": true, "readonly": true, "modal": true,
}

// triStates render as name=value when set to anything other than false, since
// "mixed" is meaningfully different from "true".
var triStates = map[string]bool{
	"checked": true, "pressed": true,
}

// Build serializes a tree into compact uid-annotated text.
//
// filter "interactive" (default) emits interactive + context nodes; "all" also
// emits static text. Nodes are walked from every root (a node no other node
// claims as a child) in document order, so uids are stable for a stable tree.
//
// known is the previous snapshot's Ref→uid binding for this surface (nil on the
// first call): an element still present keeps its uid. uidBase is the session's
// monotonic counter, from which brand-new elements are numbered; the caller
// advances it to Snapshot.NextUID.
//
// Passing known=nil and uidBase=0 every time reintroduces uid rebinding — see
// the Snapshot doc comment for why that is a correctness bug and not a cosmetic
// one.
func Build(nodes []Node, filter string, gen int, maxLines int, known map[int64]string, uidBase int) *Snapshot {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if uidBase < 0 {
		uidBase = 0
	}
	byID := make(map[string]*Node, len(nodes))
	hasParent := make(map[string]bool)
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
		for _, c := range nodes[i].ChildIDs {
			hasParent[c] = true
		}
	}

	var roots []*Node
	for i := range nodes {
		if !hasParent[nodes[i].ID] {
			roots = append(roots, &nodes[i])
		}
	}
	// A tree where every node is claimed as someone's child has no root — it is
	// malformed, or it contains a cycle reaching the top. Walking nothing would
	// hand the agent an empty snapshot and let it conclude the app has no UI,
	// which is a worse failure than rendering a slightly odd tree. The `seen`
	// guard below makes walking from every node safe.
	if len(roots) == 0 {
		for i := range nodes {
			roots = append(roots, &nodes[i])
		}
	}

	snap := &Snapshot{UIDs: make(map[string]int64), Refs: make(map[int64]string), Gen: gen}
	var lines []string
	uidSeq := uidBase
	elided := 0
	interactiveCount := 0
	// A tree with a cycle (a malformed backend, or an AX tree mutating mid-walk)
	// would otherwise recurse forever.
	seen := make(map[string]bool, len(nodes))

	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || seen[n.ID] {
			return
		}
		seen[n.ID] = true
		if !n.Ignored {
			role := n.Role
			name := strings.TrimSpace(n.Name)
			line := ""
			switch {
			case InteractiveRoles[role] && n.Ref != 0:
				// An element we have seen before keeps its uid; a new one is
				// numbered from the session counter, which never rewinds.
				uid, seen := known[n.Ref]
				if !seen {
					uidSeq++
					uid = fmt.Sprintf("e%d", uidSeq)
				}
				snap.UIDs[uid] = n.Ref
				snap.Refs[n.Ref] = uid
				interactiveCount++
				line = fmt.Sprintf("[%s] %s %q%s", uid, role, Truncate(name, 120), RenderStates(n))
			case ContextRoles[role] && name != "":
				line = fmt.Sprintf("- %s %q", role, Truncate(name, 120))
			case filter == "all" && (role == "StaticText" || role == "text") && name != "":
				line = fmt.Sprintf("  %s", Truncate(name, 160))
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
			walk(byID[cid])
		}
	}
	for _, r := range roots {
		walk(r)
	}

	if elided > 0 {
		lines = append(lines, fmt.Sprintf("… %d more nodes elided (interactive=%d, filter=%s)", elided, interactiveCount, filterOrDefault(filter)))
	}
	snap.Text = strings.Join(lines, "\n")
	snap.NextUID = uidSeq
	return snap
}

func filterOrDefault(f string) string {
	if f == "" {
		return "interactive"
	}
	return f
}

// RenderStates renders the interesting states of a node as a " (a, b=c)" suffix.
func RenderStates(n *Node) string {
	var states []string
	if v := strings.TrimSpace(n.Value); v != "" {
		states = append(states, fmt.Sprintf("value=%q", Truncate(v, 80)))
	}
	for _, p := range n.States {
		switch {
		case flagStates[p.Name]:
			if p.Value == "true" {
				states = append(states, p.Name)
			}
		case triStates[p.Name]:
			if p.Value != "" && p.Value != "false" {
				if p.Value == "true" {
					states = append(states, p.Name)
				} else {
					states = append(states, p.Name+"="+p.Value)
				}
			}
		case p.Name == "invalid":
			if p.Value != "" && p.Value != "false" {
				states = append(states, "invalid")
			}
		}
	}
	if len(states) == 0 {
		return ""
	}
	return " (" + strings.Join(states, ", ") + ")"
}

// Truncate cuts s to at most n runes, appending an ellipsis when it cuts.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
