package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ActRequest describes a single browser_act call.
type ActRequest struct {
	Action string  // click|dblclick|fill|press|hover|scroll|select|upload|dialog
	UID    string  // element uid from the latest snapshot (most actions)
	X, Y   float64 // coordinate fallback for scroll/click
	Value  string  // fill text, select value, dialog decision (accept|dismiss)
	Key    string  // for action=press (e.g. "Enter")
	Files  []string
}

// Act performs an interaction and returns a short "what changed" summary so the
// model usually does not need a follow-up snapshot.
func (s *Session) Act(ctx context.Context, req ActRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.ensureActive(ctx)
	if err != nil {
		return "", err
	}

	// Dialog handling does not need a uid.
	if req.Action == "dialog" {
		return s.handleDialog(ctx, t, req.Value)
	}

	beforeTitle, beforeURL := s.titleURL(ctx, t)

	switch req.Action {
	case "click", "dblclick", "hover", "fill", "select", "upload":
		backendID, err := s.resolveUID(t, req.UID)
		if err != nil {
			return "", err
		}
		if err := s.actOnNode(ctx, t, req, backendID); err != nil {
			return "", err
		}
	case "press":
		if err := s.pressKey(ctx, t, req.Key); err != nil {
			return "", err
		}
	case "scroll":
		if err := s.scroll(ctx, t, req); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown action %q", req.Action)
	}

	// Give the page a beat to react, then summarize the delta.
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}
	afterTitle, afterURL := s.titleURL(ctx, t)

	var b strings.Builder
	fmt.Fprintf(&b, "ok: %s", req.Action)
	if req.UID != "" {
		fmt.Fprintf(&b, " %s", req.UID)
	}
	if afterURL != beforeURL && afterURL != "" {
		fmt.Fprintf(&b, "\nnavigated → %s", afterURL)
	} else if afterTitle != beforeTitle && afterTitle != "" {
		fmt.Fprintf(&b, "\ntitle → %q", afterTitle)
	}
	if d := t.dialog; d != nil {
		fmt.Fprintf(&b, "\n[dialog %s] %q — respond with browser_act action=dialog value=accept|dismiss", d.Type, d.Message)
	}
	b.WriteString("\n(take a snapshot if you need the new element ground truth)")
	return b.String(), nil
}

// resolveUID maps a uid from the latest snapshot to a live backend node id,
// rejecting stale references.
func (s *Session) resolveUID(t *sessionTab, uid string) (int64, error) {
	if uid == "" {
		return 0, fmt.Errorf("uid is required for this action")
	}
	snap := s.snaps[t.conn.ID()]
	if snap == nil {
		return 0, fmt.Errorf("no snapshot yet; call browser_snapshot first")
	}
	backendID, ok := snap.UIDs[uid]
	if !ok {
		return 0, fmt.Errorf("uid %q not in the latest snapshot (it may be stale) — re-run browser_snapshot", uid)
	}
	return backendID, nil
}

// nodeCenter resolves a backend node id to viewport coordinates and also
// scrolls it into view.
func (s *Session) nodeCenter(ctx context.Context, t *sessionTab, backendID int64) (float64, float64, error) {
	_, _ = t.conn.Send(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": backendID})
	res, err := t.conn.Send(ctx, "DOM.getBoxModel", map[string]any{"backendNodeId": backendID})
	if err != nil {
		return 0, 0, fmt.Errorf("element not visible/available: %w", err)
	}
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := json.Unmarshal(res, &box); err != nil {
		return 0, 0, err
	}
	c := box.Model.Content
	if len(c) < 8 {
		return 0, 0, fmt.Errorf("element has no box (hidden?)")
	}
	x := (c[0] + c[2] + c[4] + c[6]) / 4
	y := (c[1] + c[3] + c[5] + c[7]) / 4
	return x, y, nil
}

func (s *Session) actOnNode(ctx context.Context, t *sessionTab, req ActRequest, backendID int64) error {
	switch req.Action {
	case "fill":
		return s.fill(ctx, t, backendID, req.Value)
	case "select":
		return s.selectOption(ctx, t, backendID, req.Value)
	case "upload":
		return s.uploadFiles(ctx, t, backendID, req.Files)
	}
	// click / dblclick / hover are coordinate-based.
	x, y, err := s.nodeCenter(ctx, t, backendID)
	if err != nil {
		return err
	}
	switch req.Action {
	case "hover":
		return s.mouse(ctx, t, "mouseMoved", x, y, 0)
	case "click":
		return s.clickAt(ctx, t, x, y, 1)
	case "dblclick":
		return s.clickAt(ctx, t, x, y, 2)
	}
	return nil
}

func (s *Session) clickAt(ctx context.Context, t *sessionTab, x, y float64, count int) error {
	if err := s.mouse(ctx, t, "mouseMoved", x, y, 0); err != nil {
		return err
	}
	if err := s.mouse(ctx, t, "mousePressed", x, y, count); err != nil {
		return err
	}
	return s.mouse(ctx, t, "mouseReleased", x, y, count)
}

func (s *Session) mouse(ctx context.Context, t *sessionTab, typ string, x, y float64, clickCount int) error {
	params := map[string]any{"type": typ, "x": x, "y": y}
	if typ != "mouseMoved" {
		params["button"] = "left"
		params["clickCount"] = clickCount
	}
	_, err := t.conn.Send(ctx, "Input.dispatchMouseEvent", params)
	return interpretErr(err)
}

func (s *Session) fill(ctx context.Context, t *sessionTab, backendID int64, value string) error {
	// Focus the field, clear it, then insert text.
	if _, err := t.conn.Send(ctx, "DOM.focus", map[string]any{"backendNodeId": backendID}); err != nil {
		// focus can fail on non-focusable wrappers; fall back to click.
		if x, y, e := s.nodeCenter(ctx, t, backendID); e == nil {
			_ = s.clickAt(ctx, t, x, y, 1)
		}
	}
	// Select-all + delete to clear existing content.
	_ = s.pressKey(ctx, t, "ctrl+a")
	_, _ = t.conn.Send(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Delete"})
	_, _ = t.conn.Send(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Delete"})
	_, err := t.conn.Send(ctx, "Input.insertText", map[string]any{"text": value})
	return interpretErr(err)
}

func (s *Session) selectOption(ctx context.Context, t *sessionTab, backendID int64, value string) error {
	// Resolve to a JS object then set value + dispatch change.
	res, err := t.conn.Send(ctx, "DOM.resolveNode", map[string]any{"backendNodeId": backendID})
	if err != nil {
		return err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(res, &rn); err != nil {
		return err
	}
	_, err = t.conn.Send(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId": rn.Object.ObjectID,
		"functionDeclaration": `function(v){
			const opt = Array.from(this.options||[]).find(o=>o.value===v||o.label===v||o.text===v);
			if(opt){this.value=opt.value;} else {this.value=v;}
			this.dispatchEvent(new Event('input',{bubbles:true}));
			this.dispatchEvent(new Event('change',{bubbles:true}));
			return this.value;
		}`,
		"arguments": []any{map[string]any{"value": value}},
	})
	return interpretErr(err)
}

// uploadFiles sets files on an <input type=file> via CDP (bypasses the OS
// chooser). Approval for upload is enforced by the tool/approval layer.
func (s *Session) uploadFiles(ctx context.Context, t *sessionTab, backendID int64, files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("upload requires files")
	}
	_, err := t.conn.Send(ctx, "DOM.setFileInputFiles", map[string]any{
		"backendNodeId": backendID,
		"files":         files,
	})
	return interpretErr(err)
}

func (s *Session) pressKey(ctx context.Context, t *sessionTab, key string) error {
	if key == "" {
		return fmt.Errorf("press requires a key")
	}
	mods := 0
	parts := strings.Split(key, "+")
	main := parts[len(parts)-1]
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mods |= 2
		case "shift":
			mods |= 8
		case "alt":
			mods |= 1
		case "meta", "cmd":
			mods |= 4
		}
	}
	down := map[string]any{"type": "keyDown", "key": normalizeKey(main)}
	up := map[string]any{"type": "keyUp", "key": normalizeKey(main)}
	if mods != 0 {
		down["modifiers"] = mods
		up["modifiers"] = mods
	}
	if _, err := t.conn.Send(ctx, "Input.dispatchKeyEvent", down); err != nil {
		return interpretErr(err)
	}
	_, err := t.conn.Send(ctx, "Input.dispatchKeyEvent", up)
	return interpretErr(err)
}

func normalizeKey(k string) string {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "Enter"
	case "tab":
		return "Tab"
	case "escape", "esc":
		return "Escape"
	case "backspace":
		return "Backspace"
	case "space":
		return " "
	}
	return k
}

func (s *Session) scroll(ctx context.Context, t *sessionTab, req ActRequest) error {
	dy := req.Y
	if dy == 0 {
		dy = 600 // default one "page" down
	}
	x, y := req.X, req.Y
	if x == 0 {
		x = 400
	}
	if y == 0 {
		y = 400
	}
	_, err := t.conn.Send(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": x, "y": y, "deltaX": req.X, "deltaY": dy,
	})
	return interpretErr(err)
}

func (s *Session) handleDialog(ctx context.Context, t *sessionTab, decision string) (string, error) {
	if t.dialog == nil {
		return "", fmt.Errorf("no pending dialog")
	}
	accept := decision == "accept" || decision == "ok" || decision == "true"
	params := map[string]any{"accept": accept}
	if _, err := t.conn.Send(ctx, "Page.handleJavaScriptDialog", params); err != nil {
		return "", interpretErr(err)
	}
	kind := t.dialog.Type
	t.dialog = nil
	verb := "dismissed"
	if accept {
		verb = "accepted"
	}
	return fmt.Sprintf("ok: %s %s dialog", verb, kind), nil
}

// interpretErr maps a detach/close CDP error to ErrControlInterrupted so tools
// can report user takeover naturally.
func interpretErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "detached") || strings.Contains(msg, "target closed") ||
		strings.Contains(msg, "connection closed") || strings.Contains(msg, "not attached") {
		return ErrControlInterrupted
	}
	return err
}
