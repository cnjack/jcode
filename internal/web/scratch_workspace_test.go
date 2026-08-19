package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
	managedworkspace "github.com/cnjack/jcode/internal/workspace"
)

func TestNewScratchSessionAllocatesManagedWorkspace(t *testing.T) {
	s := stubFactoryServer(t)
	baseFactory := s.newEngine
	s.newScratchEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		cfg, err := baseFactory(taskID, pwd, modeName)
		if err != nil {
			return nil, err
		}
		cfg.WorkspaceKind = session.WorkspaceScratch
		if cfg.Recorder != nil {
			cfg.Recorder.SetWorkspaceKind(session.WorkspaceScratch)
		}
		return cfg, nil
	}

	create := func(body string) activationResult {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost, "/api/sessions", strings.NewReader(body),
		)
		s.handleNewSession(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create scratch: code=%d body=%q", rec.Code, rec.Body.String())
		}
		var result activationResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := create(`{"workspace_kind":"scratch"}`)
	// An older client sends an empty body for New Task. The active scratch
	// classification still requires a fresh managed directory.
	second := create("")
	if first.WorkspaceKind != session.WorkspaceScratch || second.WorkspaceKind != session.WorkspaceScratch {
		t.Fatalf("unexpected workspace kinds: first=%q second=%q", first.WorkspaceKind, second.WorkspaceKind)
	}
	if first.Pwd == second.Pwd {
		t.Fatalf("scratch sessions reused workspace %q", first.Pwd)
	}
	for _, path := range []string{first.Pwd, second.Pwd} {
		if err := managedworkspace.ValidateScratchPath(path); err != nil {
			t.Fatalf("invalid managed workspace %q: %v", path, err)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("scratch workspace missing: path=%q info=%v err=%v", path, info, err)
		}
	}
	if got := s.activeEngine().workspaceKind; got != session.WorkspaceScratch {
		t.Fatalf("active engine kind=%q, want scratch", got)
	}

	// The activation endpoint is also used by Cloud/mobile new-chat commands.
	// With no explicit target it must inherit scratch semantics and allocate a
	// third directory, not build a project engine inside the active scratch path.
	activateRec := httptest.NewRecorder()
	activateReq := httptest.NewRequest(http.MethodPost, "/api/sessions/activate", strings.NewReader(`{}`))
	s.handleActivateSession(activateRec, activateReq)
	if activateRec.Code != http.StatusOK {
		t.Fatalf("activate scratch: code=%d body=%q", activateRec.Code, activateRec.Body.String())
	}
	var activated activationResult
	if err := json.Unmarshal(activateRec.Body.Bytes(), &activated); err != nil {
		t.Fatal(err)
	}
	if activated.WorkspaceKind != session.WorkspaceScratch || activated.Pwd == second.Pwd {
		t.Fatalf("activation did not allocate fresh scratch workspace: %+v", activated)
	}
}

func TestNewSessionRejectsUnknownWorkspaceKind(t *testing.T) {
	s := stubFactoryServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost, "/api/sessions", strings.NewReader(`{"workspace_kind":"temporary"}`),
	)
	s.handleNewSession(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNewProjectSessionRejectsActiveManagedScratchPath(t *testing.T) {
	s := stubFactoryServer(t)
	baseFactory := s.newEngine
	s.newScratchEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		cfg, err := baseFactory(taskID, pwd, modeName)
		if err != nil {
			return nil, err
		}
		cfg.WorkspaceKind = session.WorkspaceScratch
		cfg.Recorder.SetWorkspaceKind(session.WorkspaceScratch)
		return cfg, nil
	}

	createScratch := httptest.NewRecorder()
	s.handleNewSession(createScratch, httptest.NewRequest(
		http.MethodPost, "/api/sessions", strings.NewReader(`{"workspace_kind":"scratch"}`),
	))
	if createScratch.Code != http.StatusOK {
		t.Fatalf("create scratch: code=%d body=%q", createScratch.Code, createScratch.Body.String())
	}

	rec := httptest.NewRecorder()
	s.handleNewSession(rec, httptest.NewRequest(
		http.MethodPost, "/api/sessions", strings.NewReader(`{"workspace_kind":"project"}`),
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}
