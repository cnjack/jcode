package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
	mempipeline "github.com/cnjack/jcode/internal/memory/pipeline"
)

type memoryConfigPayload struct {
	Enabled             bool   `json:"enabled"`
	Generate            bool   `json:"generate"`
	Model               string `json:"model"`
	DailyTokenBudget    int    `json:"daily_token_budget"`
	CooldownHours       int    `json:"cooldown_hours"`
	MaxAgeDays          int    `json:"max_age_days"`
	MaxUnusedDays       int    `json:"max_unused_days"`
	Phase2TopN          int    `json:"phase2_top_n"`
	SummaryInjectTokens int    `json:"summary_inject_tokens"`
}

type memoryConfigRequest struct {
	Enabled             *bool  `json:"enabled"`
	Generate            *bool  `json:"generate"`
	Model               string `json:"model"`
	DailyTokenBudget    int    `json:"daily_token_budget"`
	CooldownHours       int    `json:"cooldown_hours"`
	MaxAgeDays          int    `json:"max_age_days"`
	MaxUnusedDays       int    `json:"max_unused_days"`
	Phase2TopN          int    `json:"phase2_top_n"`
	SummaryInjectTokens int    `json:"summary_inject_tokens"`
}

type memoryProjectRequest struct {
	Project string `json:"project"`
}

const (
	memorySyncFailedWarning = "The last memory consolidation failed. Check the debug log for details."
	memoryRefreshWarning    = "Memory changed, but open tasks could not be refreshed. Start a new task before relying on it."
)

func (r memoryConfigRequest) payload() memoryConfigPayload {
	return memoryConfigPayload{
		Enabled:             *r.Enabled,
		Generate:            *r.Generate,
		Model:               r.Model,
		DailyTokenBudget:    r.DailyTokenBudget,
		CooldownHours:       r.CooldownHours,
		MaxAgeDays:          r.MaxAgeDays,
		MaxUnusedDays:       r.MaxUnusedDays,
		Phase2TopN:          r.Phase2TopN,
		SummaryInjectTokens: r.SummaryInjectTokens,
	}
}

func resolvedMemoryConfig(cfg *config.Config) memoryConfigPayload {
	mc := cfg.MemorySettings()
	return memoryConfigPayload{
		Enabled:             config.MemoryEnabled(cfg),
		Generate:            config.MemoryGenerateSetting(cfg),
		Model:               mc.Model,
		DailyTokenBudget:    config.MemoryDailyTokenBudget(cfg),
		CooldownHours:       config.MemoryCooldownHours(cfg),
		MaxAgeDays:          config.MemoryMaxAgeDays(cfg),
		MaxUnusedDays:       config.MemoryMaxUnusedDays(cfg),
		Phase2TopN:          config.MemoryPhase2TopN(cfg),
		SummaryInjectTokens: config.MemorySummaryInjectTokens(cfg),
	}
}

func (p memoryConfigPayload) storedConfig() *config.MemoryConfig {
	enabled, generate := p.Enabled, p.Generate
	return &config.MemoryConfig{
		Enabled:             &enabled,
		Generate:            &generate,
		Model:               strings.TrimSpace(p.Model),
		DailyTokenBudget:    p.DailyTokenBudget,
		CooldownHours:       p.CooldownHours,
		MaxAgeDays:          p.MaxAgeDays,
		MaxUnusedDays:       p.MaxUnusedDays,
		Phase2TopN:          p.Phase2TopN,
		SummaryInjectTokens: p.SummaryInjectTokens,
	}
}

func validateMemoryConfig(p memoryConfigPayload) error {
	if p.Model != strings.TrimSpace(p.Model) || strings.ContainsAny(p.Model, "\r\n\x00") || len(p.Model) > 512 {
		return errors.New("model must be a single trimmed provider/model value of at most 512 characters")
	}
	if p.Model != "" {
		parts := strings.SplitN(p.Model, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return errors.New("model must be empty or use provider/model format")
		}
	}
	checks := []struct {
		name       string
		value, min int
		max        int
	}{
		{"daily_token_budget", p.DailyTokenBudget, 1, 10_000_000},
		{"cooldown_hours", p.CooldownHours, 1, 24 * 30},
		{"max_age_days", p.MaxAgeDays, 1, 3650},
		{"max_unused_days", p.MaxUnusedDays, 1, 3650},
		{"phase2_top_n", p.Phase2TopN, 1, 1000},
		{"summary_inject_tokens", p.SummaryInjectTokens, 64, 100_000},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%s must be between %d and %d", check.name, check.min, check.max)
		}
	}
	return nil
}

func (s *Server) memoryProject() (project string, remote bool, ok bool) {
	eng := s.activeEngine()
	if eng == nil || eng.pwd == "" {
		return "", false, false
	}
	return eng.pwd, eng.env != nil && eng.env.IsRemote(), true
}

func (s *Server) memoryRunning(project string) bool {
	s.memoryRunMu.Lock()
	defer s.memoryRunMu.Unlock()
	return s.memoryRuns[project]
}

func (s *Server) memoryWarning(project string) string {
	s.memoryRunMu.Lock()
	defer s.memoryRunMu.Unlock()
	return s.memoryWarnings[project]
}

func (s *Server) setMemoryWarning(project, warning string) {
	s.memoryRunMu.Lock()
	if s.memoryWarnings == nil {
		s.memoryWarnings = make(map[string]string)
	}
	if warning == "" {
		delete(s.memoryWarnings, project)
	} else {
		s.memoryWarnings[project] = warning
	}
	s.memoryRunMu.Unlock()
}

func (s *Server) reserveMemoryOperation(project string) bool {
	s.memoryRunMu.Lock()
	defer s.memoryRunMu.Unlock()
	if s.memoryRuns == nil {
		s.memoryRuns = make(map[string]bool)
	}
	if s.memoryRuns[project] {
		return false
	}
	s.memoryRuns[project] = true
	return true
}

func (s *Server) releaseMemoryOperation(project string) {
	s.memoryRunMu.Lock()
	delete(s.memoryRuns, project)
	s.memoryRunMu.Unlock()
}

func (s *Server) handleMemoryStatus(w http.ResponseWriter, _ *http.Request) {
	project, remote, ok := s.memoryProject()
	s.cfgMu.Lock()
	cfg := s.cfg
	resolved := resolvedMemoryConfig(cfg)
	effectiveGenerate := config.MemoryGenerate(cfg)
	s.cfgMu.Unlock()
	response := map[string]any{
		// Global Memory configuration remains editable without a local active
		// project (including while the foreground task is remote). "supported"
		// separately gates current-project status/sync/clear operations.
		"available":          cfg != nil,
		"supported":          ok && !remote,
		"remote":             remote,
		"running":            ok && !remote && s.memoryRunning(project),
		"project":            project,
		"config":             resolved,
		"effective_generate": effectiveGenerate,
	}
	if ok && !remote {
		if warning := s.memoryWarning(project); warning != "" {
			response["warning"] = warning
		}
	}
	if !ok {
		response["error"] = "no active task"
		writeJSON(w, http.StatusOK, response)
		return
	}
	if remote {
		response["error"] = "memory management is only available for local projects"
		writeJSON(w, http.StatusOK, response)
		return
	}

	root := memory.ProjectRoot(project)
	state := memory.LoadState(root)
	summary, statErr := os.Stat(filepath.Join(root, memory.SummaryFile))
	response["summary_exists"] = statErr == nil && !summary.IsDir()
	if statErr == nil && !summary.IsDir() {
		response["summary_size"] = summary.Size()
		response["summary_modified_at"] = summary.ModTime().Format(time.RFC3339)
	} else {
		response["summary_size"] = int64(0)
		response["summary_modified_at"] = ""
	}
	response["notes_count"] = countMemoryNotes(root)
	response["tracked_files"] = len(state.Files)
	response["extracted_count"] = len(state.Extracted)
	failed := 0
	for _, record := range state.Extracted {
		if record != nil && record.Failed {
			failed++
		}
	}
	response["failed_count"] = failed
	response["today_tokens"] = state.Budget[time.Now().Format("2006-01-02")]
	response["last_pipeline_at"] = state.LastPipelineAt
	if state.LastConsolidation != nil {
		response["last_consolidation_at"] = state.LastConsolidation.At
		response["last_consolidation_noop"] = state.LastConsolidation.NoopFastPath
		response["last_consolidation_decisions"] = state.LastConsolidation.Decisions
	} else {
		response["last_consolidation_at"] = ""
		response["last_consolidation_noop"] = false
		response["last_consolidation_decisions"] = map[string]int{}
	}
	writeJSON(w, http.StatusOK, response)
}

func countMemoryNotes(scopeRoot string) int {
	entries, err := os.ReadDir(filepath.Join(scopeRoot, memory.NotesDir))
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			count++
		}
	}
	return count
}

func (s *Server) handleMemoryConfig(w http.ResponseWriter, r *http.Request) {
	var raw memoryConfigRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid memory config: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if raw.Enabled == nil || raw.Generate == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled and generate are required"})
		return
	}
	req := raw.payload()
	if err := validateMemoryConfig(req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.MemoryConfigSnapshot()
	s.cfg.SetMemory(req.storedConfig())
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfg.SetMemory(previous)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response := map[string]any{"status": "ok", "config": resolvedMemoryConfig(s.cfg)}
	// Every memory setting uses one rebuild path. Enabled changes the memory_note
	// schema; prompt-affecting settings are re-rendered by the command factory.
	if err := s.rebuildToolAgents(); err != nil {
		config.Logger().Printf("[memory] config saved but active-agent refresh failed: %v", err)
		response["warning_code"] = "agent_refresh_failed"
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleMemorySync(w http.ResponseWriter, r *http.Request) {
	var req memoryProjectRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.Project) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project is required"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	project, remote, ok := s.memoryProject()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	if req.Project != project {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "active project changed; refresh Memory settings and try again"})
		return
	}
	if remote {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory sync is only available for local projects"})
		return
	}
	if s.memoryStart == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory sync unavailable"})
		return
	}
	s.cfgMu.Lock()
	enabled := config.MemoryGenerate(s.cfg)
	s.cfgMu.Unlock()
	if !enabled {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "memory generation is disabled"})
		return
	}
	if !s.reserveMemoryOperation(project) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "memory pipeline is already running"})
		return
	}
	s.setMemoryWarning(project, "")

	root := s.rootCtx()
	if root == nil {
		root = context.Background()
	}
	ctx, cancel := context.WithTimeout(root, 20*time.Minute)
	done, err := s.memoryStart(ctx, project)
	if err != nil {
		cancel()
		s.releaseMemoryOperation(project)
		if errors.Is(err, mempipeline.ErrAlreadyRunning) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "memory pipeline is already running"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if done == nil {
		cancel()
		s.releaseMemoryOperation(project)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "memory sync returned no completion handle"})
		return
	}
	go func() {
		defer cancel()
		defer s.releaseMemoryOperation(project)
		err, ok := <-done
		if !ok {
			err = nil
		}
		if err != nil && !errors.Is(err, mempipeline.ErrAlreadyRunning) {
			config.Logger().Printf("[memory] manual web sync failed: %v", err)
			s.setMemoryWarning(project, memorySyncFailedWarning)
			return
		}
		if rebuildErr := s.rebuildToolAgents(); rebuildErr != nil {
			config.Logger().Printf("[memory] sync completed but active-agent refresh failed: %v", rebuildErr)
			s.setMemoryWarning(project, memoryRefreshWarning)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	if scope := r.URL.Query().Get("scope"); scope != "project" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope must be project"})
		return
	}
	project, remote, ok := s.memoryProject()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	if expected := r.URL.Query().Get("project"); expected == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project is required"})
		return
	} else if expected != project {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "active project changed; refresh Memory settings and try again"})
		return
	}
	if remote {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "memory clear is only available for local projects"})
		return
	}
	if !s.reserveMemoryOperation(project) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "memory pipeline is running"})
		return
	}
	defer s.releaseMemoryOperation(project)
	busy, err := memory.ClearScope(memory.ProjectRoot(project))
	if busy {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "memory pipeline is running"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.setMemoryWarning(project, "")
	response := map[string]any{"status": "cleared", "scope": "project"}
	if rebuildErr := s.rebuildToolAgents(); rebuildErr != nil {
		config.Logger().Printf("[memory] clear completed but active-agent refresh failed: %v", rebuildErr)
		s.setMemoryWarning(project, memoryRefreshWarning)
		response["warning_code"] = "agent_refresh_failed"
		response["warning"] = memoryRefreshWarning
	}
	writeJSON(w, http.StatusOK, response)
}
