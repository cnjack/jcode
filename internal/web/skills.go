package web

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"

	"github.com/cnjack/jcode/internal/config"
)

// --- Skills list handler (for slash commands) ---

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	type skillItem struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Slash       string `json:"slash"`
		Builtin     bool   `json:"builtin"`
		Source      string `json:"source"` // builtin | local
		Enabled     bool   `json:"enabled"`
	}

	var items []skillItem
	if s.skillLoader != nil {
		for _, sk := range s.skillLoader.All() {
			source := "local"
			if sk.Builtin {
				source = "builtin"
			}
			items = append(items, skillItem{
				Name:        sk.Name,
				Description: sk.Description,
				Slash:       sk.Slash,
				Builtin:     sk.Builtin,
				Source:      source,
				Enabled:     s.skillLoader.IsEnabled(sk.Name),
			})
		}
	}
	if items == nil {
		items = []skillItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleToggleSkill enables/disables a skill, persisting to config and updating
// the loader + agent so the change takes effect immediately.
func (s *Server) handleToggleSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if s.skillLoader == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "skills unavailable"})
		return
	}

	// cfgMu (not s.mu) serializes the cfg read-modify-write+save, so concurrent
	// approval-mode / MCP / skill saves can't clobber each other in memory or on
	// disk.
	s.cfgMu.Lock()
	// Rebuild the disabled set from config.
	disabled := make(map[string]bool, len(s.cfg.DisabledSkills))
	for _, n := range s.cfg.DisabledSkills {
		disabled[n] = true
	}
	if req.Enabled {
		delete(disabled, name)
	} else {
		disabled[name] = true
	}
	list := make([]string, 0, len(disabled))
	for n := range disabled {
		list = append(list, n)
	}
	sort.Strings(list)
	s.cfg.DisabledSkills = list
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.cfgMu.Unlock()

	s.skillLoader.SetDisabled(list)
	// Rebuild the foreground task's agent so the system prompt (skill descriptions)
	// and load_skill tool reflect the change on the next run.
	if !s.needsSetup {
		if eng := s.activeEngine(); eng != nil && eng.createAgent != nil {
			prov, mod, _ := eng.modelSnapshot()
			if ag, err := eng.createAgent(prov, mod); err == nil {
				eng.setAgent(ag)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name, "enabled": req.Enabled})
}

// handleSlashCommands returns skill slash commands for the web frontend
// autocomplete menu. Built-in commands (/setting, /model, /ssh, etc.) are
// excluded because the web UI provides dedicated controls for those features
// and submitMessage only dispatches skill-based slash commands.
func (s *Server) handleSlashCommands(w http.ResponseWriter, r *http.Request) {
	type slashItem struct {
		Slash       string `json:"slash"`
		Description string `json:"description"`
		Type        string `json:"type"` // "skill" | "flow"
	}

	var items []slashItem
	if s.skillLoader != nil {
		for _, sk := range s.skillLoader.SlashCommands() {
			items = append(items, slashItem{
				Slash:       sk.Slash,
				Description: sk.Description,
				Type:        "skill",
			})
		}
	}
	// Workflows resolve against the foreground task's project so its
	// .jcode/workflows show up in autocomplete, falling back to the boot loader.
	if fl := s.flowLoaderFor(s.activeEngine()); fl != nil {
		for _, fc := range fl.SlashCommands() {
			items = append(items, slashItem{
				Slash:       fc.Slash,
				Description: fc.Description,
				Type:        "flow",
			})
		}
	}

	if items == nil {
		items = []slashItem{}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Slash < items[j].Slash
	})

	writeJSON(w, http.StatusOK, items)
}
