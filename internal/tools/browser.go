package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/browser"
)

// NewBrowserTools returns the browser-use tool set for this Env. When browser
// use is unavailable or disabled, the tools are absent from the model schema.
func (e *Env) NewBrowserTools() []tool.BaseTool {
	if e.Browser == nil {
		return nil
	}
	cfg := e.Browser.GetConfig()
	if !cfg.Enabled {
		return nil
	}
	tools := []tool.BaseTool{
		&browserTool{env: e, info: browserOpenInfo()},
		&browserTool{env: e, info: browserSnapshotInfo()},
		&browserTool{env: e, info: browserScreenshotInfo()},
		&browserTool{env: e, info: browserActInfo()},
		&browserTool{env: e, info: browserReadInfo()},
		&browserTool{env: e, info: browserTabsInfo()},
	}
	if cfg.DevMode {
		tools = append(tools, &browserTool{env: e, info: browserEvalInfo()})
	}
	return tools
}

// NewBrowserPlanTools returns the read-only browser subset for plan mode:
// navigation (GET) + inspection, no interaction or eval.
func (e *Env) NewBrowserPlanTools() []tool.BaseTool {
	if e.Browser == nil || !e.Browser.Enabled() {
		return nil
	}
	return []tool.BaseTool{
		&browserTool{env: e, info: browserPlanOpenInfo(), planOnly: true},
		&browserTool{env: e, info: browserSnapshotInfo(), planOnly: true},
		&browserTool{env: e, info: browserScreenshotInfo(), planOnly: true},
		&browserTool{env: e, info: browserReadInfo(), planOnly: true},
		&browserTool{env: e, info: browserPlanTabsInfo(), planOnly: true},
	}
}

type browserTool struct {
	env      *Env
	info     *schema.ToolInfo
	planOnly bool
}

func (t *browserTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *browserTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	if t.planOnly {
		if err := validatePlanBrowserCall(t.info.Name, argsJSON); err != nil {
			return "", err
		}
	}
	sess, err := t.env.BrowserSession(ctx)
	if err != nil {
		return "", err
	}
	out, err := dispatchBrowser(ctx, t.env, sess, t.info.Name, argsJSON)
	if errors.Is(err, browser.ErrControlInterrupted) {
		// Report naturally; the model should stop rather than retry.
		return "Browser control was interrupted (the extension or user took over). Stopping browser work.", nil
	}
	return out, err
}

func validatePlanBrowserCall(name, argsJSON string) error {
	switch name {
	case "browser_open":
		var input struct {
			NewTab bool `json:"new_tab"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return fmt.Errorf("invalid browser_open args: %w", err)
		}
		if input.NewTab {
			return fmt.Errorf("browser_open new_tab=true is not allowed in Plan mode")
		}
	case "browser_tabs":
		var input struct {
			Op string `json:"op"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return fmt.Errorf("invalid browser_tabs args: %w", err)
		}
		switch input.Op {
		case "", "list", "select":
			return nil
		default:
			return fmt.Errorf("browser_tabs op %q is not allowed in Plan mode; use list or select", input.Op)
		}
	}
	return nil
}

func dispatchBrowser(ctx context.Context, env *Env, sess *browser.Session, name, argsJSON string) (string, error) {
	switch name {
	case "browser_open":
		var in struct {
			URL    string `json:"url"`
			NewTab bool   `json:"new_tab"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		if strings.TrimSpace(in.URL) == "" {
			return "", fmt.Errorf("url is required")
		}
		return sess.Open(ctx, in.URL, in.NewTab)

	case "browser_snapshot":
		var in struct {
			Filter   string `json:"filter"`
			MaxLines int    `json:"max_lines"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		return sess.Snapshot(ctx, in.Filter, in.MaxLines)

	case "browser_screenshot":
		var in struct {
			FullPage bool `json:"full_page"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		png, err := sess.Screenshot(ctx, in.FullPage)
		if err != nil {
			return "", err
		}
		id, err := env.Browser.SaveScreenshot(png)
		if err != nil {
			return "", err
		}
		// The web UI renders image_ref inline; text clients see the ref + size.
		return fmt.Sprintf("[screenshot %dx? bytes=%d image_ref=/api/browser/shots/%s.png]\nCaptured. The image is shown in the UI; use browser_snapshot for element ground truth.", len(png), len(png), id), nil

	case "browser_act":
		return browserAct(ctx, sess, argsJSON)

	case "browser_read":
		var in struct {
			Kind  string `json:"kind"`
			Limit int    `json:"limit"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		switch in.Kind {
		case "", "text":
			return sess.PageText(ctx, in.Limit)
		case "console", "network":
			return "", fmt.Errorf("read kind %q is not yet available; use browser_snapshot or browser_read kind=text", in.Kind)
		default:
			return "", fmt.Errorf("unknown read kind %q (use text)", in.Kind)
		}

	case "browser_tabs":
		return browserTabs(ctx, sess, argsJSON)

	case "browser_eval":
		if !env.Browser.DevMode() {
			return "", fmt.Errorf("browser_eval requires developer mode (enable it in browser settings)")
		}
		var in struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		if strings.TrimSpace(in.Expression) == "" {
			return "", fmt.Errorf("expression is required")
		}
		return sess.Eval(ctx, in.Expression)
	}
	return "", fmt.Errorf("unknown browser tool %q", name)
}

func browserAct(ctx context.Context, sess *browser.Session, argsJSON string) (string, error) {
	var in struct {
		Action string   `json:"action"`
		UID    string   `json:"uid"`
		Value  string   `json:"value"`
		Key    string   `json:"key"`
		X      float64  `json:"x"`
		Y      float64  `json:"y"`
		Files  []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if in.Action == "" {
		return "", fmt.Errorf("action is required")
	}
	if in.Action == "reload" {
		// Reload navigates the active tab and returns a fresh snapshot header.
		return sess.Reload(ctx)
	}
	return sess.Act(ctx, browser.ActRequest{
		Action: in.Action, UID: in.UID, Value: in.Value,
		Key: in.Key, X: in.X, Y: in.Y, Files: in.Files,
	})
}

func browserTabs(ctx context.Context, sess *browser.Session, argsJSON string) (string, error) {
	var in struct {
		Op    string `json:"op"`
		TabID string `json:"tab_id"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &in)
	switch in.Op {
	case "", "list":
		tabs, err := sess.ListTabs(ctx)
		if err != nil {
			return "", err
		}
		if len(tabs) == 0 {
			return "(no tabs)", nil
		}
		var b strings.Builder
		for _, t := range tabs {
			mark := " "
			if t.Attached {
				mark = "*"
			}
			flag := ""
			if t.UserTab {
				flag = " [user]"
			}
			fmt.Fprintf(&b, "%s %s  %q  %s%s\n", mark, shortTabID(t.ID), t.Title, t.URL, flag)
		}
		b.WriteString("(* = controlled by jcode)")
		return b.String(), nil
	case "new":
		id, err := sess.NewTab(ctx)
		if err != nil {
			return "", err
		}
		return "opened tab " + shortTabID(id), nil
	case "select":
		return "selected tab " + shortTabID(in.TabID), sess.SelectTab(ctx, in.TabID)
	case "claim":
		return "claimed tab " + shortTabID(in.TabID), sess.ClaimTab(ctx, in.TabID)
	case "close":
		return "closed tab " + shortTabID(in.TabID), sess.CloseTab(ctx, in.TabID)
	default:
		return "", fmt.Errorf("unknown tabs op %q", in.Op)
	}
}

func shortTabID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// --- Tool schemas ---

func strParam(desc string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{Type: schema.String, Desc: desc, Required: required}
}
func boolParam(desc string) *schema.ParameterInfo {
	return &schema.ParameterInfo{Type: schema.Boolean, Desc: desc, Required: false}
}
func intParam(desc string) *schema.ParameterInfo {
	return &schema.ParameterInfo{Type: schema.Integer, Desc: desc, Required: false}
}
func numParam(desc string) *schema.ParameterInfo {
	return &schema.ParameterInfo{Type: schema.Number, Desc: desc, Required: false}
}

func browserOpenInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_open",
		Desc: "Open a URL in the browser and return a snapshot header (title + top interactive elements). " +
			"Use for localhost dev verification and general web navigation. If already on the URL, use browser_act action=reload instead of re-opening.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url":     strParam("The URL to open (http/https).", true),
			"new_tab": boolParam("Open in a new tab instead of the active one. Default false."),
		}),
	}
}

func browserPlanOpenInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_open",
		Desc: "Open a URL in the currently controlled browser tab and return a snapshot header. " +
			"Plan mode cannot create a new tab.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": strParam("The URL to open in the active tab (http/https).", true),
		}),
	}
}

func browserSnapshotInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_snapshot",
		Desc: "Return a compact text snapshot of the current page: interactive elements each tagged with a uid like [e3] " +
			"that browser_act targets. This is your primary way to see the page. Re-snapshot after navigation or when an action fails.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"filter":    strParam("interactive (default) or all (also include static text).", false),
			"max_lines": intParam("Max element lines before eliding (default 400)."),
		}),
	}
}

func browserScreenshotInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name:        "browser_screenshot",
		Desc:        "Capture a PNG screenshot of the current page. Use for visual confirmation only; prefer browser_snapshot for element ground truth.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"full_page": boolParam("Capture the full page instead of the viewport. Default false.")}),
	}
}

func browserActInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_act",
		Desc: "Perform one interaction on the page. Reference elements by the uid from the latest browser_snapshot. " +
			"Returns a summary of what changed (navigation, dialog, etc.). Actions: click, dblclick, fill, press, hover, scroll, select, upload, dialog, reload.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": strParam("One of: click, dblclick, fill, press, hover, scroll, select, upload, dialog, reload.", true),
			"uid":    strParam("Element uid from the latest snapshot (e.g. e3). Required for click/fill/select/upload/hover.", false),
			"value":  strParam("Text for fill; option value for select; accept|dismiss for dialog.", false),
			"key":    strParam("Key for action=press (e.g. Enter, Tab, ctrl+a).", false),
			"x":      numParam("X coordinate / horizontal delta for scroll."),
			"y":      numParam("Y coordinate / vertical delta for scroll (default one page)."),
			"files": {Type: schema.Array, Desc: "Absolute file paths for action=upload.", Required: false,
				ElemInfo: &schema.ParameterInfo{Type: schema.String}},
		}),
	}
}

func browserReadInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_read",
		Desc: "Read the current page's visible body text, bounded by a character limit.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"limit": intParam("Maximum characters to return (default 20000)."),
		}),
	}
}

func browserTabsInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_tabs",
		Desc: "Manage tabs. op=list shows tabs (* = controlled by jcode, [user] = pre-existing). " +
			"op=new opens a blank tab; select switches; claim takes over a user tab (extension backend); close closes a controlled tab.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"op":     strParam("list (default), new, select, claim, close.", false),
			"tab_id": strParam("Tab id (short prefix ok) for select/claim/close.", false),
		}),
	}
}

func browserPlanTabsInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "browser_tabs",
		Desc: "Inspect or select an existing browser tab in Plan mode. " +
			"op=list shows tabs and op=select switches to an existing controlled tab.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"op": {
				Type:     schema.String,
				Desc:     "list (default) or select.",
				Enum:     []string{"list", "select"},
				Required: false,
			},
			"tab_id": strParam("Existing tab id (short prefix ok) for select.", false),
		}),
	}
}

func browserEvalInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name:        "browser_eval",
		Desc:        "Evaluate a read-only JavaScript expression in the page and return its JSON value. Requires developer mode; always prompts for approval.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"expression": strParam("A read-only JS expression.", true)}),
	}
}
