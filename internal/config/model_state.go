package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const modelStateFile = "model_state.json"

// ModelState tracks recent, favorite, and visibility settings for models.
type ModelState struct {
	Recent   []ModelRef `json:"recent,omitempty"`
	Favorite []ModelRef `json:"favorite,omitempty"`
	// EnabledModels lists models explicitly enabled by the user (shown in model selector).
	// If nil/empty, default-enabled models from the registry are used.
	EnabledModels []ModelRef `json:"enabled_models,omitempty"`
	// DisabledModels lists models explicitly disabled by the user (hidden from model selector).
	DisabledModels []ModelRef `json:"disabled_models,omitempty"`
}

// ModelRef uniquely identifies a model in "provider/model" format.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

var (
	modelStateMu sync.Mutex
)

// modelStatePath returns the path to the model state file.
func modelStatePath() string {
	return filepath.Join(ConfigDir(), modelStateFile)
}

// LoadModelState loads the model state from disk.
func LoadModelState() (*ModelState, error) {
	modelStateMu.Lock()
	defer modelStateMu.Unlock()

	p := modelStatePath()

	data, err := os.ReadFile(p)
	if err != nil {
		return &ModelState{}, nil
	}

	var state ModelState
	if err := json.Unmarshal(data, &state); err != nil {
		return &ModelState{}, nil
	}
	return &state, nil
}

// SaveModelState writes the model state to disk.
func SaveModelState(state *ModelState) error {
	modelStateMu.Lock()
	defer modelStateMu.Unlock()

	p := modelStatePath()

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// AddRecent adds a model to the recent list (deduped, max 10).
func (s *ModelState) AddRecent(ref ModelRef) {
	// Remove if already present
	filtered := make([]ModelRef, 0, len(s.Recent))
	for _, r := range s.Recent {
		if r.Provider != ref.Provider || r.Model != ref.Model {
			filtered = append(filtered, r)
		}
	}
	// Prepend
	s.Recent = append([]ModelRef{ref}, filtered...)
	// Cap at 10
	if len(s.Recent) > 10 {
		s.Recent = s.Recent[:10]
	}
}

// ToggleFavorite adds or removes a model from favorites. Returns true if now favorite.
func (s *ModelState) ToggleFavorite(ref ModelRef) bool {
	for i, r := range s.Favorite {
		if r.Provider == ref.Provider && r.Model == ref.Model {
			s.Favorite = append(s.Favorite[:i], s.Favorite[i+1:]...)
			return false
		}
	}
	s.Favorite = append(s.Favorite, ref)
	return true
}

// IsFavorite returns whether the given model is in the favorites list.
func (s *ModelState) IsFavorite(ref ModelRef) bool {
	for _, r := range s.Favorite {
		if r.Provider == ref.Provider && r.Model == ref.Model {
			return true
		}
	}
	return false
}

// IsModelEnabled returns whether the given model should be shown in the model selector.
// Logic: if the model is in EnabledModels, it's enabled.
// If the model is in DisabledModels, it's disabled.
// Otherwise, fallback to the defaultEnabled parameter (from registry).
func (s *ModelState) IsModelEnabled(ref ModelRef, defaultEnabled bool) bool {
	for _, r := range s.DisabledModels {
		if r.Provider == ref.Provider && r.Model == ref.Model {
			return false
		}
	}
	for _, r := range s.EnabledModels {
		if r.Provider == ref.Provider && r.Model == ref.Model {
			return true
		}
	}
	return defaultEnabled
}

// SetModelEnabled explicitly enables or disables a model in the model selector.
func (s *ModelState) SetModelEnabled(ref ModelRef, enabled bool) {
	// Remove from both lists first
	s.EnabledModels = removeModelRef(s.EnabledModels, ref)
	s.DisabledModels = removeModelRef(s.DisabledModels, ref)

	if enabled {
		s.EnabledModels = append(s.EnabledModels, ref)
	} else {
		s.DisabledModels = append(s.DisabledModels, ref)
	}
}

// removeModelRef removes a model ref from a slice.
func removeModelRef(refs []ModelRef, ref ModelRef) []ModelRef {
	result := make([]ModelRef, 0, len(refs))
	for _, r := range refs {
		if r.Provider != ref.Provider || r.Model != ref.Model {
			result = append(result, r)
		}
	}
	return result
}
