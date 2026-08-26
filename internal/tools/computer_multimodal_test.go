package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/uitree"
)

func TestComputerScreenshotToolReturnsTextAndPNG(t *testing.T) {
	const bundleID = "com.example.Canvas"
	home := t.TempDir()
	fake := computer.NewFake()
	fake.SetApps(computer.App{Name: "Canvas", BundleID: bundleID})
	fake.SetTree(bundleID, []uitree.Node{{Role: "button", Name: "OK", Ref: 101}})
	png := []byte("\x89PNG\r\n\x1a\nmultimodal-test")
	fake.SetVisualShot(bundleID, computer.Screenshot{
		PNG: png, X: 120, Y: 80, Width: 900, Height: 600, PixelWidth: 1800, PixelHeight: 1200,
	})

	mgr := computer.NewManager(computer.Config{Enabled: true}, home)
	mgr.SetFakeBackend(fake)
	env := NewEnv(t.TempDir(), "darwin")
	env.Computer = mgr

	var open tool.InvokableTool
	var screenshot tool.EnhancedInvokableTool
	for _, candidate := range env.NewComputerTools() {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		switch info.Name {
		case "computer_open":
			open, _ = candidate.(tool.InvokableTool)
		case "computer_screenshot":
			screenshot, _ = candidate.(tool.EnhancedInvokableTool)
			if _, standard := candidate.(tool.InvokableTool); standard {
				t.Fatal("computer_screenshot must use only the enhanced result path")
			}
		}
	}
	if open == nil || screenshot == nil {
		t.Fatalf("tool wiring incomplete: open=%v screenshot=%v", open != nil, screenshot != nil)
	}
	if _, err := open.InvokableRun(context.Background(), `{"app":"`+bundleID+`"}`); err != nil {
		t.Fatalf("computer_open: %v", err)
	}

	result, err := screenshot.InvokableRun(context.Background(), &schema.ToolArgument{
		Text: `{"app":"` + bundleID + `"}`,
	})
	if err != nil {
		t.Fatalf("computer_screenshot: %v", err)
	}
	if len(result.Parts) != 2 {
		t.Fatalf("parts=%d, want text+image", len(result.Parts))
	}
	if result.Parts[0].Type != schema.ToolPartTypeText ||
		!strings.Contains(result.Parts[0].Text, "image_ref=/api/computer/shots/") ||
		!strings.Contains(result.Parts[0].Text, "PNG is attached") ||
		!strings.Contains(result.Parts[0].Text, "x=120.0 y=80.0") ||
		!strings.Contains(result.Parts[0].Text, "1800x1200 pixels") {
		t.Fatalf("unexpected text part: %#v", result.Parts[0])
	}
	image := result.Parts[1]
	if image.Type != schema.ToolPartTypeImage || image.Image == nil {
		t.Fatalf("unexpected image part: %#v", image)
	}
	if image.Image.MIMEType != "image/png" || image.Image.Base64Data == nil {
		t.Fatalf("unexpected image metadata: %#v", image.Image)
	}
	decoded, err := base64.StdEncoding.DecodeString(*image.Image.Base64Data)
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded PNG=%q, want %q", decoded, png)
	}

	shotDir := filepath.Join(home, ".jcode", "computer", "shots")
	entries, err := os.ReadDir(shotDir)
	if err != nil {
		t.Fatalf("read shot dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("saved shots=%d, want 1", len(entries))
	}
	saved, err := os.ReadFile(filepath.Join(shotDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read saved shot: %v", err)
	}
	if !bytes.Equal(saved, png) {
		t.Fatalf("saved PNG=%q, want %q", saved, png)
	}
}

func TestComputerPlanScreenshotIsEnhanced(t *testing.T) {
	env := NewEnv(t.TempDir(), "darwin")
	env.Computer = computer.NewManager(computer.Config{Enabled: true}, t.TempDir())
	env.Computer.SetFakeBackend(computer.NewFake())
	for _, candidate := range env.NewComputerPlanTools() {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Name == "computer_screenshot" {
			if _, ok := candidate.(tool.EnhancedInvokableTool); !ok {
				t.Fatal("plan-mode computer_screenshot is not enhanced")
			}
			return
		}
	}
	t.Fatal("plan-mode computer_screenshot missing")
}
