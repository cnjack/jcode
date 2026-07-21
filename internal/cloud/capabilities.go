// capabilities.go builds the device-capabilities mirror (M12): the compose
// facets the local control plane can offer a remotely-started session —
// projects (from the session index), models (from config + the model
// registry, mirroring GET /api/models) and the supported reasoning-effort
// levels. The connector reports it as the top-level `capabilities` field of
// every sessions upsert; the orchestrator stores it in devices.capabilities.
package cloud

import (
	"path/filepath"
	"sort"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
)

// DeviceCapabilities is the capabilities payload stored by the orchestrator
// and consumed by the console/mobile compose UI.
type DeviceCapabilities struct {
	Projects []CapabilityProject `json:"projects"`
	Models   []CapabilityModel   `json:"models"`
	Efforts  []string            `json:"efforts"`
}

// CapabilityProject is one known project directory.
type CapabilityProject struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// CapabilityModel is one selectable model of a configured provider.
type CapabilityModel struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Label    string `json:"label"`
}

// standardEfforts mirrors the registry's standard effort option set
// (internal/model standardEffortOptions), used when no configured model
// advertises reasoning options.
var standardEfforts = []string{"minimal", "low", "medium", "high"}

// collectCapabilities builds the capabilities mirror. It is best-effort per
// source: a failure in one facet is logged and that facet reports empty —
// capabilities must never break the session upsert they ride along with.
func (c *Connector) collectCapabilities() *DeviceCapabilities {
	caps := &DeviceCapabilities{
		Projects: []CapabilityProject{},
		Models:   []CapabilityModel{},
		Efforts:  []string{},
	}

	// Projects: the session index is keyed by project path — the same source
	// the sessions upsert uses (there is no separate projects endpoint).
	listFn := c.cfg.ListSessionsFn
	if listFn == nil {
		listFn = session.ListAllSessions
	}
	if all, err := listFn(); err != nil {
		c.logf("capabilities: session index unavailable: %v", err)
	} else {
		for path := range all {
			if path == "" {
				continue
			}
			caps.Projects = append(caps.Projects, CapabilityProject{Path: path, Name: filepath.Base(path)})
		}
		sort.Slice(caps.Projects, func(i, j int) bool { return caps.Projects[i].Path < caps.Projects[j].Path })
	}

	modelsFn := c.cfg.ModelCapabilitiesFn
	if modelsFn == nil {
		modelsFn = collectModelCapabilities
	}
	models, efforts, err := modelsFn()
	if err != nil {
		c.logf("capabilities: model list unavailable: %v", err)
	} else {
		caps.Models = models
		caps.Efforts = efforts
	}
	return caps
}

// collectModelCapabilities lists the selectable models of the configured
// providers, mirroring the web control plane's GET /api/models: registry
// models of configured providers, filtered by the user's enabled/disabled
// model state. Efforts are the union of the listed models' reasoning-effort
// options (falling back to the standard set when no model advertises any).
func collectModelCapabilities() ([]CapabilityModel, []string, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, err
	}
	registry := model.NewModelRegistryWithConfig(cfg)
	modelState, _ := config.LoadModelState()
	configured := cfg.GetProviders()

	models := []CapabilityModel{}
	efforts := []string{}
	effortSeen := map[string]bool{}
	for _, rp := range registry.ListProviders() {
		if _, ok := configured[rp.ID]; !ok {
			continue
		}
		for _, m := range registry.ListProviderModels(rp.ID, true) {
			ref := config.ModelRef{Provider: rp.ID, Model: m.ID}
			if !modelState.IsModelEnabled(ref, m.DefaultEnabled) {
				continue
			}
			label := m.Name
			if label == "" {
				label = m.ID
			}
			models = append(models, CapabilityModel{Provider: rp.ID, ID: m.ID, Label: label})
			for _, ro := range m.ReasoningOptions {
				if ro.Type != "effort" {
					continue
				}
				for _, v := range ro.Values {
					if !effortSeen[v] {
						effortSeen[v] = true
						efforts = append(efforts, v)
					}
				}
			}
		}
	}
	if len(efforts) == 0 {
		efforts = standardEfforts
	}
	return models, efforts, nil
}
