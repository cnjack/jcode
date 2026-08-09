package tui

import "testing"

func TestBillableApprovalResponseEchoesExactOpaqueOption(t *testing.T) {
	m := &Model{approvalOptions: []ToolApprovalOption{
		{ID: "runner-allow-once", Label: "Allow once", Kind: "allow_once"},
		{ID: "runner-deny", Label: "Deny", Kind: "deny"},
	}}
	allow := m.approvalResponse(true, ModeManual)
	if !allow.Approved || allow.Mode != ModeManual || allow.ResolvedOptionID != "runner-allow-once" {
		t.Fatalf("allow response = %#v", allow)
	}
	deny := m.approvalResponse(false, ModeManual)
	if deny.Approved || deny.Mode != ModeManual || deny.ResolvedOptionID != "runner-deny" {
		t.Fatalf("deny response = %#v", deny)
	}
}

func TestBillableApprovalResponseFailsClosedWithoutMatchingOption(t *testing.T) {
	m := &Model{approvalOptions: []ToolApprovalOption{
		{ID: "custom", Label: "Custom", Kind: "custom"},
	}}
	if response := m.approvalResponse(true, ModeManual); response.ResolvedOptionID != "" {
		t.Fatalf("unexpected option echo = %#v", response)
	}
}
