package uitree

import (
	"strings"
	"testing"
	"time"
)

func nodes() []Node {
	return []Node{
		{ID: "1", Role: "window", Name: "App", ChildIDs: []string{"2", "3", "4"}},
		{ID: "2", Role: "button", Name: "Save", Ref: 101},
		{ID: "3", Role: "textbox", Name: "Name", Ref: 102, States: []State{{"focused", "true"}}},
		{ID: "4", Role: "StaticText", Name: "hello"},
	}
}

func TestBuildMintsUIDsForInteractiveNodes(t *testing.T) {
	s := Build(nodes(), "interactive", 1, 100, nil, 0)
	if len(s.UIDs) != 2 {
		t.Fatalf("expected 2 uids, got %d (%v)", len(s.UIDs), s.UIDs)
	}
	if s.UIDs["e1"] != 101 || s.UIDs["e2"] != 102 {
		t.Errorf("uid→ref mapping wrong: %v", s.UIDs)
	}
	if s.Refs[101] != "e1" || s.Refs[102] != "e2" {
		t.Errorf("ref→uid mapping wrong: %v", s.Refs)
	}
	if s.NextUID != 2 {
		t.Errorf("NextUID = %d, want 2", s.NextUID)
	}
	if !strings.Contains(s.Text, `[e1] button "Save"`) {
		t.Errorf("text missing the button line:\n%s", s.Text)
	}
	if !strings.Contains(s.Text, "focused") {
		t.Errorf("text missing the focused state:\n%s", s.Text)
	}
	// StaticText is only emitted under filter=all.
	if strings.Contains(s.Text, "hello") {
		t.Errorf("filter=interactive leaked static text:\n%s", s.Text)
	}
}

func TestBuildFilterAllIncludesStaticText(t *testing.T) {
	s := Build(nodes(), "all", 1, 100, nil, 0)
	if !strings.Contains(s.Text, "hello") {
		t.Errorf("filter=all dropped static text:\n%s", s.Text)
	}
}

// The core invariant: a uid names an element. Survivors keep theirs; the
// departed never hand theirs on.
func TestUIDsAreBoundToElementsNotPositions(t *testing.T) {
	first := Build(nodes(), "interactive", 1, 100, nil, 0)

	// "Save" (ref 101) is replaced by a different button in the same position;
	// "Name" (ref 102) survives.
	mutated := []Node{
		{ID: "1", Role: "window", Name: "App", ChildIDs: []string{"2", "3"}},
		{ID: "2", Role: "button", Name: "Delete Everything", Ref: 999},
		{ID: "3", Role: "textbox", Name: "Name", Ref: 102},
	}
	second := Build(mutated, "interactive", 2, 100, first.Refs, first.NextUID)

	if got := second.UIDs["e1"]; got != 0 {
		t.Errorf("e1 was reissued to ref %d; a retired uid must never come back", got)
	}
	if second.Refs[999] == "e1" {
		t.Errorf("the replacement element inherited the retired uid e1: %v", second.Refs)
	}
	if second.Refs[102] != "e2" {
		t.Errorf("a surviving element lost its uid: %v (want e2)", second.Refs[102])
	}
	if second.UIDs[second.Refs[999]] != 999 {
		t.Errorf("the new element did not get a working uid: %v", second.UIDs)
	}
}

// Two consecutive Builds of an unchanged tree must be textually identical, or
// the caller's diff reports the whole tree as churn every time.
func TestUnchangedTreeIsTextuallyStable(t *testing.T) {
	first := Build(nodes(), "interactive", 1, 100, nil, 0)
	second := Build(nodes(), "interactive", 2, 100, first.Refs, first.NextUID)
	if first.Text != second.Text {
		t.Errorf("an unchanged tree produced different text:\n--- first ---\n%s\n--- second ---\n%s", first.Text, second.Text)
	}
}

func TestBuildElidesBeyondMaxLines(t *testing.T) {
	var big []Node
	root := Node{ID: "0", Role: "window", Name: "App"}
	for i := 1; i <= 10; i++ {
		id := string(rune('a' + i))
		root.ChildIDs = append(root.ChildIDs, id)
		big = append(big, Node{ID: id, Role: "button", Name: "b", Ref: int64(i)})
	}
	big = append([]Node{root}, big...)
	s := Build(big, "interactive", 1, 3, nil, 0)
	if !strings.Contains(s.Text, "elided") {
		t.Errorf("expected an elision marker:\n%s", s.Text)
	}
	// Elided nodes still get uids — the model can reach them after narrowing.
	if len(s.UIDs) != 10 {
		t.Errorf("elision dropped uids: got %d, want 10", len(s.UIDs))
	}
}

// A malformed backend, or a tree mutating mid-walk, must not hang the agent.
func TestBuildSurvivesCycles(t *testing.T) {
	// A real root, with a cycle below it: child → grandchild → back to child.
	cyclic := []Node{
		{ID: "root", Role: "window", Name: "A", ChildIDs: []string{"1"}},
		{ID: "1", Role: "button", Name: "B", Ref: 1, ChildIDs: []string{"2"}},
		{ID: "2", Role: "button", Name: "C", Ref: 2, ChildIDs: []string{"1"}},
	}
	done := make(chan *Snapshot, 1)
	go func() { done <- Build(cyclic, "interactive", 1, 100, nil, 0) }()
	select {
	case s := <-done:
		if len(s.UIDs) != 2 {
			t.Errorf("expected 2 uids, got %v", s.UIDs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Build did not return on a cyclic tree — it is looping")
	}
}

// A tree in which every node is someone's child has no root. Rendering nothing
// would tell the agent the app has no UI at all, which is a worse lie than an
// oddly-ordered tree.
func TestRootlessTreeStillRenders(t *testing.T) {
	rootless := []Node{
		{ID: "1", Role: "window", Name: "A", ChildIDs: []string{"2"}},
		{ID: "2", Role: "button", Name: "B", Ref: 1, ChildIDs: []string{"1"}},
	}
	done := make(chan *Snapshot, 1)
	go func() { done <- Build(rootless, "interactive", 1, 100, nil, 0) }()
	select {
	case s := <-done:
		if len(s.UIDs) != 1 {
			t.Errorf("a rootless tree rendered %d uids, want 1 (%v)\n%s", len(s.UIDs), s.UIDs, s.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Build did not return on a rootless tree — it is looping")
	}
}

func TestIgnoredNodesAreSkipped(t *testing.T) {
	n := nodes()
	n[1].Ignored = true
	s := Build(n, "interactive", 1, 100, nil, 0)
	if strings.Contains(s.Text, "Save") {
		t.Errorf("an ignored node was emitted:\n%s", s.Text)
	}
	if len(s.UIDs) != 1 {
		t.Errorf("an ignored node was given a uid: %v", s.UIDs)
	}
}

// A node with no backend handle cannot be acted on, so giving it a uid would
// promise something the backend cannot honor.
func TestNodesWithoutRefGetNoUID(t *testing.T) {
	s := Build([]Node{{ID: "1", Role: "button", Name: "Ghost"}}, "interactive", 1, 100, nil, 0)
	if len(s.UIDs) != 0 {
		t.Errorf("a node with Ref=0 got a uid: %v", s.UIDs)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("Truncate short = %q", got)
	}
	if got := Truncate("hello world", 5); got != "hello…" {
		t.Errorf("Truncate long = %q", got)
	}
	// Must cut on a rune boundary, not a byte one.
	if got := Truncate("你好世界", 2); got != "你好…" {
		t.Errorf("Truncate multibyte = %q", got)
	}
}

func TestRenderStates(t *testing.T) {
	n := &Node{
		Value: "hi",
		States: []State{
			{"focused", "true"}, {"disabled", "false"},
			{"checked", "mixed"}, {"pressed", "true"}, {"invalid", "true"},
			{"irrelevant", "true"},
		},
	}
	got := RenderStates(n)
	for _, want := range []string{`value="hi"`, "focused", "checked=mixed", "pressed", "invalid"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderStates missing %q: %s", want, got)
		}
	}
	for _, bad := range []string{"disabled", "irrelevant"} {
		if strings.Contains(got, bad) {
			t.Errorf("RenderStates included %q: %s", bad, got)
		}
	}
}
