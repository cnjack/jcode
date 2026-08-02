package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/artifact"
)

type artifactRecorderFake struct{ records []artifact.Record }

func (f *artifactRecorderFake) RecordArtifact(record artifact.Record) error {
	f.records = append(f.records, record)
	return nil
}

func TestShowArtifactRegistersBeforeEmittingWebEvent(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "report.html"), []byte("<h1>Ready</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := &artifactRecorderFake{}
	service := artifact.NewService(nil, func() time.Time {
		return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	})
	var eventName string
	var eventRecord artifact.Record
	env := NewEnv(workspace, "darwin")
	tool := env.NewShowArtifactTool(&ShowArtifactDeps{
		SessionID: func() string { return "task-1" },
		Recorder:  recorder,
		Service:   service,
		Emit: func(event string, data any) {
			if len(recorder.records) != 1 {
				t.Fatal("event emitted before durable record")
			}
			eventName = event
			eventRecord = data.(artifact.Record)
		},
	})

	output, err := tool.InvokableRun(context.Background(), `{"path":"report.html","title":"Demo","kind":"auto","focus":true}`)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("output is not JSON: %v: %s", err, output)
	}
	if response["artifact_id"] == "" || response["revision"] != float64(1) || eventName != "artifact_upserted" || eventRecord.Title != "Demo" {
		t.Fatalf("response=%v event=%q record=%+v", response, eventName, eventRecord)
	}
}

func TestShowArtifactSchemaExplainsWebDeliveryAndNoCloudUpload(t *testing.T) {
	env := NewEnv(t.TempDir(), "darwin")
	tool := env.NewShowArtifactTool(&ShowArtifactDeps{})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "show_artifact" || !strings.Contains(info.Desc, "Web/Desktop") || !strings.Contains(info.Desc, "does not upload") {
		t.Fatalf("tool info=%+v", info)
	}
}

func TestShowArtifactCanForceNoFocusForAutomation(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "report.md"), []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := &artifactRecorderFake{}
	var emitted artifact.Record
	tool := NewEnv(workspace, "darwin").NewShowArtifactTool(&ShowArtifactDeps{
		SessionID:    func() string { return "automation-task" },
		Recorder:     recorder,
		Service:      artifact.NewService(nil, time.Now),
		ForceNoFocus: true,
		Emit: func(_ string, data any) {
			emitted = data.(artifact.Record)
		},
	})
	if _, err := tool.InvokableRun(context.Background(), `{"path":"report.md","focus":true}`); err != nil {
		t.Fatal(err)
	}
	if emitted.Focus {
		t.Fatal("automation artifact events must never request foreground focus")
	}
}
