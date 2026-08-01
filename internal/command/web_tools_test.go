package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// stubTool is a minimal tool.BaseTool (Info only) for testing tool-set filtering
// without standing up a real Env / model.
type stubTool struct {
	name string
}

func TestShowArtifactCandidateIsRegisteredOnlyByWebTransport(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	dir := filepath.Dir(currentFile)
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if count := strings.Count(read("web.go"), "NewShowArtifactTool"); count != 2 {
		t.Fatalf("web candidate count=%d want all+plan", count)
	}
	for _, name := range []string{"interactive.go", "acp.go"} {
		if strings.Contains(read(name), "NewShowArtifactTool") {
			t.Fatalf("%s must not register show_artifact", name)
		}
	}
}

func (s stubTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.name}, nil
}

// Tools implementing BaseTool compile as tool.BaseTool.
var _ tool.BaseTool = stubTool{}

// TestDropInteractiveTools verifies that automation runs drop tools that require a
// live human (ask_user) while keeping every other tool. This is the guard against
// an unattended run stalling on a question nobody is watching.
func TestDropInteractiveTools(t *testing.T) {
	all := []tool.BaseTool{
		stubTool{name: "read"},
		stubTool{name: "ask_user"},
		stubTool{name: "edit"},
		stubTool{name: "execute"},
	}

	got := dropInteractiveTools(all)
	if len(got) != 3 {
		t.Fatalf("want 3 tools after dropping ask_user, got %d", len(got))
	}
	for _, tl := range got {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Name == "ask_user" {
			t.Fatalf("ask_user was not dropped from automation tool set")
		}
	}
}

// TestDropInteractiveToolsKeepsAllWhenNoInteractive confirms the filter is a no-op
// for a tool set with nothing to drop (so normal task tool lists are unaffected).
func TestDropInteractiveToolsKeepsAllWhenNoInteractive(t *testing.T) {
	all := []tool.BaseTool{
		stubTool{name: "read"},
		stubTool{name: "edit"},
	}
	got := dropInteractiveTools(all)
	if len(got) != len(all) {
		t.Fatalf("want %d tools (nothing to drop), got %d", len(all), len(got))
	}
}
