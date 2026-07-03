package browser

import (
	"strings"
	"testing"
)

func node(id, role, name string, backend int64, children ...string) axNode {
	return axNode{
		NodeID:           id,
		Role:             &axValue{Value: role},
		Name:             &axValue{Value: name},
		BackendDOMNodeID: backend,
		ChildIDs:         children,
	}
}

func TestBuildSnapshotAssignsUIDsToInteractiveNodes(t *testing.T) {
	nodes := []axNode{
		node("1", "RootWebArea", "Doc", 0, "2", "3", "4"),
		node("2", "link", "Files changed", 101),
		node("3", "button", "Merge", 102),
		node("4", "heading", "Pull Request", 0),
	}
	snap := buildSnapshot(nodes, "interactive", 1, 100)

	if len(snap.UIDs) != 2 {
		t.Fatalf("expected 2 uids, got %d (%v)", len(snap.UIDs), snap.UIDs)
	}
	if snap.UIDs["e1"] != 101 || snap.UIDs["e2"] != 102 {
		t.Fatalf("uid→backend mapping wrong: %v", snap.UIDs)
	}
	if !strings.Contains(snap.Text, `[e1] link "Files changed"`) {
		t.Errorf("missing link line:\n%s", snap.Text)
	}
	if !strings.Contains(snap.Text, `[e2] button "Merge"`) {
		t.Errorf("missing button line:\n%s", snap.Text)
	}
	// heading is a context role → shown without uid.
	if !strings.Contains(snap.Text, `- heading "Pull Request"`) {
		t.Errorf("missing heading context line:\n%s", snap.Text)
	}
}

func TestBuildSnapshotRendersStates(t *testing.T) {
	n := node("2", "button", "Merge", 102)
	n.Properties = []axProp{{Name: "disabled", Value: &axValue{Value: "true"}}}
	tb := node("3", "textbox", "Comment", 103)
	tb.Value = &axValue{Value: "hi"}
	cb := node("4", "checkbox", "Viewed", 104)
	cb.Properties = []axProp{{Name: "checked", Value: &axValue{Value: "true"}}}

	nodes := []axNode{
		node("1", "RootWebArea", "Doc", 0, "2", "3", "4"), n, tb, cb,
	}
	snap := buildSnapshot(nodes, "interactive", 1, 100)
	if !strings.Contains(snap.Text, "(disabled)") {
		t.Errorf("disabled state missing:\n%s", snap.Text)
	}
	if !strings.Contains(snap.Text, `value="hi"`) {
		t.Errorf("value state missing:\n%s", snap.Text)
	}
	if !strings.Contains(snap.Text, "(checked)") {
		t.Errorf("checked state missing:\n%s", snap.Text)
	}
}

func TestBuildSnapshotElidesBeyondMaxLines(t *testing.T) {
	nodes := []axNode{node("root", "RootWebArea", "Doc", 0)}
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		nodes[0].ChildIDs = append(nodes[0].ChildIDs, id)
		nodes = append(nodes, node(id, "button", "b", int64(100+i)))
	}
	snap := buildSnapshot(nodes, "interactive", 1, 3)
	if !strings.Contains(snap.Text, "more nodes elided") {
		t.Errorf("expected elision marker with maxLines=3:\n%s", snap.Text)
	}
	// UIDs are still minted for elided nodes (so a later, larger snapshot is
	// not required to act) — but the visible lines are capped.
	visible := strings.Count(snap.Text, "[e")
	if visible > 4 {
		t.Errorf("expected <=4 visible uid lines, got %d", visible)
	}
}

func TestOriginOf(t *testing.T) {
	cases := map[string]string{
		"https://github.com/jack/jcode/pull/105": "https://github.com",
		"http://localhost:3000/app":              "http://localhost:3000",
		"about:blank":                            "",
		"":                                       "",
		"file:///tmp/x.html":                     "",
	}
	for in, want := range cases {
		if got := OriginOf(in); got != want {
			t.Errorf("OriginOf(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsLocalOrigin(t *testing.T) {
	local := []string{"http://localhost:3000", "http://127.0.0.1", "https://app.localhost"}
	remote := []string{"https://github.com", "https://example.com:8443"}
	for _, o := range local {
		if !IsLocalOrigin(o) {
			t.Errorf("%q should be local", o)
		}
	}
	for _, o := range remote {
		if IsLocalOrigin(o) {
			t.Errorf("%q should not be local", o)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("no truncation expected, got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate got %q", got)
	}
}
