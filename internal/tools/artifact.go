package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/artifact"
)

type ShowArtifactDeps struct {
	SessionID    func() string
	Recorder     artifact.Recorder
	Service      *artifact.Service
	Emit         func(event string, data any)
	ForceNoFocus bool
}

type ShowArtifactInput struct {
	Path  string        `json:"path"`
	Title string        `json:"title,omitempty"`
	Kind  artifact.Kind `json:"kind,omitempty"`
	Focus *bool         `json:"focus,omitempty"`
}

// NewShowArtifactTool creates the Web-only delivery tool. Transport scoping is
// enforced by command registration and the command tool catalog; this method
// contains no global enable switch.
func (e *Env) NewShowArtifactTool(deps *ShowArtifactDeps) tool.InvokableTool {
	return &showArtifactTool{env: e, deps: deps, info: &schema.ToolInfo{
		Name: "show_artifact",
		Desc: `Register a finished, user-consumable workspace file in the Web/Desktop Artifacts viewer.

Call this only after writing and validating the final report, visualization, image, PDF, or data file. Do not register routine source edits, logs, temporary files, or build output. The path must be relative to the current local workspace. Re-register a path after a meaningful update. This tool records local metadata and does not upload or share anything with Cloud.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type: schema.String, Required: true,
				Desc: "Slash-separated path to an existing file, relative to the current workspace.",
			},
			"title": {Type: schema.String, Desc: "Optional human-readable title (maximum 200 characters)."},
			"kind": {
				Type: schema.String, Desc: "Optional renderer hint; auto detects from content and extension.",
				Enum: []string{"auto", "text", "markdown", "code", "html", "image", "pdf", "csv", "binary"},
			},
			"focus": {Type: schema.Boolean, Desc: "Open the viewer for the active task. Defaults to true."},
		}),
	}}
}

type showArtifactTool struct {
	env  *Env
	deps *ShowArtifactDeps
	info *schema.ToolInfo
}

func (t *showArtifactTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *showArtifactTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if t.env.IsRemote() {
		return "", fmt.Errorf("artifact preview is not available for remote workspaces yet")
	}
	if t.deps == nil || t.deps.Service == nil || t.deps.Recorder == nil || t.deps.SessionID == nil {
		return "", fmt.Errorf("artifact preview is not available in this context")
	}
	var input ShowArtifactInput
	decoder := json.NewDecoder(strings.NewReader(argumentsInJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", fmt.Errorf("invalid show_artifact arguments: %w", err)
	}
	focus := true
	if input.Focus != nil {
		focus = *input.Focus
	}
	if t.deps.ForceNoFocus {
		focus = false
	}
	record, err := t.deps.Service.Register(ctx, artifact.RegisterRequest{
		SessionID: t.deps.SessionID(), Workspace: t.env.Pwd(), RelativePath: input.Path,
		Title: input.Title, Kind: input.Kind, Focus: focus,
	}, t.deps.Recorder)
	if err != nil {
		return "", err
	}
	if t.deps.Emit != nil {
		t.deps.Emit("artifact_upserted", record)
	}
	output, err := json.Marshal(map[string]any{
		"artifact_id": record.ID, "path": record.RelativePath, "title": record.Title,
		"kind": record.Kind, "revision": record.Revision,
		"message": "Artifact is available in the Artifacts panel.",
	})
	if err != nil {
		return "", err
	}
	return string(output), nil
}
