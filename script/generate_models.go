package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	modelsDevURL = "https://models.dev/api.json"
	outputFile   = "registry_generated.go"
)

// wantedProviders defines the provider IDs to include in the generated registry.
// The order here determines the display order in the setup wizard.
// All IDs must match exactly with models.dev provider IDs.
var wantedProviders = []string{
	// International providers
	"openai",
	"anthropic",
	"google",
	"deepseek",
	"zhipuai",
	"zhipuai-coding-plan",
	"mistral",
	"openrouter",
	"groq",
	"togetherai",
	// Chinese providers
	"alibaba-cn",
	"alibaba-coding-plan-cn",
	"moonshotai",
	"minimax",
	"minimax-coding-plan",
	"siliconflow",
	"tencent-coding-plan",
	"tencent-tokenhub",
	"zai",
	"zai-coding-plan",
	"xiaomi",
	"xiaomi-token-plan-cn",
	// Cloud/local
	"ollama-cloud",
}

// RegistryProvider represents a provider from models.dev API.
type RegistryProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Env    []string                  `json:"env"`
	API    string                    `json:"api"`
	Doc    string                    `json:"doc,omitempty"`
	Models map[string]*RegistryModel `json:"models"`
}

// RegistryModel represents a model from models.dev API.
type RegistryModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Family           string            `json:"family,omitempty"`
	Attachment       bool              `json:"attachment,omitempty"`
	Reasoning        bool              `json:"reasoning,omitempty"`
	ToolCall         bool              `json:"tool_call,omitempty"`
	StructuredOutput bool              `json:"structured_output,omitempty"`
	Temperature      bool              `json:"temperature,omitempty"`
	Knowledge        string            `json:"knowledge,omitempty"`
	ReleaseDate      string            `json:"release_date,omitempty"`
	LastUpdated      string            `json:"last_updated,omitempty"`
	Modalities       *ModelModalities  `json:"modalities,omitempty"`
	OpenWeights      bool              `json:"open_weights,omitempty"`
	Cost             *ModelCost        `json:"cost,omitempty"`
	Limit            *ModelLimit       `json:"limit,omitempty"`
	Status           string            `json:"status,omitempty"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
}

// ReasoningOption mirrors models.dev's reasoning_options entries.
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    *int     `json:"min,omitempty"`
	Max    *int     `json:"max,omitempty"`
}

// ModelModalities describes input/output modalities.
type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// ModelCost describes per-token costs in USD per 1M tokens.
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// ModelLimit describes context window and output limits.
type ModelLimit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Printf("Fetching models from %s...\n", modelsDevURL)

	resp, err := http.Get(modelsDevURL) // #nosec G107
	if err != nil {
		return fmt.Errorf("fetching models.dev API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models.dev API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var providers map[string]*RegistryProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	fmt.Printf("Fetched %d providers\n", len(providers))

	// Filter to only wanted providers, preserving order
	wantedSet := make(map[string]bool, len(wantedProviders))
	for _, id := range wantedProviders {
		wantedSet[id] = true
	}
	filteredProviders := make(map[string]*RegistryProvider)
	var orderedIDs []string
	for _, id := range wantedProviders {
		if prov, ok := providers[id]; ok {
			filteredProviders[id] = prov
			orderedIDs = append(orderedIDs, id)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: wanted provider %q not found in models.dev\n", id)
		}
	}
	fmt.Printf("Filtered to %d wanted providers\n", len(filteredProviders))
	providers = filteredProviders

	// Apply API URL overrides
	apiOverrides := map[string]string{
		"minimax":             "https://api.minimax.io/v1",
		"minimax-coding-plan": "https://api.minimax.io/v1",
	}
	for id, url := range apiOverrides {
		if prov, ok := providers[id]; ok {
			prov.API = url
			fmt.Printf("Override API URL for %q: %s\n", id, url)
		}
	}

	// Generate Go source code
	var b strings.Builder
	b.WriteString("// Code generated by script/generate_models.go; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Generated at: %s\n\n", time.Now().Format(time.RFC3339))
	b.WriteString("package model\n\n")

	// Generate ordered provider ID list (preserves display order for setup wizard)
	b.WriteString("// generatedProviderOrder is the display order for providers in the setup wizard.\n")
	b.WriteString("var generatedProviderOrder = []string{\n")
	for _, id := range orderedIDs {
		fmt.Fprintf(&b, "\t%q,\n", id)
	}
	b.WriteString("}\n\n")

	b.WriteString("// generatedProviders contains the static model registry data from models.dev.\n")
	b.WriteString("var generatedProviders = map[string]*RegistryProvider{\n")

	// Use ordered IDs for deterministic output
	providerIDs := orderedIDs
	sort.Strings(providerIDs)

	for _, provID := range providerIDs {
		prov := providers[provID]
		fmt.Fprintf(&b, "\t%q: {\n", provID)
		fmt.Fprintf(&b, "\t\tID: %q,\n", prov.ID)
		fmt.Fprintf(&b, "\t\tName: %q,\n", prov.Name)

		if len(prov.Env) > 0 {
			b.WriteString("\t\tEnv: []string{")
			for i, env := range prov.Env {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q", env)
			}
			b.WriteString("},\n")
		}

		fmt.Fprintf(&b, "\t\tAPI: %q,\n", prov.API)

		if prov.Doc != "" {
			fmt.Fprintf(&b, "\t\tDoc: %q,\n", escapeString(prov.Doc))
		}

		if len(prov.Models) > 0 {
			b.WriteString("\t\tModels: map[string]*RegistryModel{\n")

			// Sort model IDs for deterministic output
			modelIDs := make([]string, 0, len(prov.Models))
			for id := range prov.Models {
				modelIDs = append(modelIDs, id)
			}
			sort.Strings(modelIDs)

			for _, modelID := range modelIDs {
				model := prov.Models[modelID]
				fmt.Fprintf(&b, "\t\t\t%q: {\n", modelID)
				fmt.Fprintf(&b, "\t\t\t\tID: %q,\n", model.ID)
				fmt.Fprintf(&b, "\t\t\t\tName: %q,\n", model.Name)

				if model.Family != "" {
					fmt.Fprintf(&b, "\t\t\t\tFamily: %q,\n", model.Family)
				}
				if model.Attachment {
					b.WriteString("\t\t\t\tAttachment: true,\n")
				}
				if model.Reasoning {
					b.WriteString("\t\t\t\tReasoning: true,\n")
				}
				if model.ToolCall {
					b.WriteString("\t\t\t\tToolCall: true,\n")
				}
				if model.StructuredOutput {
					b.WriteString("\t\t\t\tStructuredOutput: true,\n")
				}
				if model.Temperature {
					b.WriteString("\t\t\t\tTemperature: true,\n")
				}
				if model.Knowledge != "" {
					fmt.Fprintf(&b, "\t\t\t\tKnowledge: %q,\n", model.Knowledge)
				}
				if model.ReleaseDate != "" {
					fmt.Fprintf(&b, "\t\t\t\tReleaseDate: %q,\n", model.ReleaseDate)
				}
				if model.LastUpdated != "" {
					fmt.Fprintf(&b, "\t\t\t\tLastUpdated: %q,\n", model.LastUpdated)
				}
				if model.OpenWeights {
					b.WriteString("\t\t\t\tOpenWeights: true,\n")
				}
				if model.Status != "" {
					fmt.Fprintf(&b, "\t\t\t\tStatus: %q,\n", model.Status)
				}

				if model.Modalities != nil {
					b.WriteString("\t\t\t\tModalities: &ModelModalities{\n")
					if len(model.Modalities.Input) > 0 {
						b.WriteString("\t\t\t\t\tInput: []string{")
						for i, m := range model.Modalities.Input {
							if i > 0 {
								b.WriteString(", ")
							}
							fmt.Fprintf(&b, "%q", m)
						}
						b.WriteString("},\n")
					}
					if len(model.Modalities.Output) > 0 {
						b.WriteString("\t\t\t\t\tOutput: []string{")
						for i, m := range model.Modalities.Output {
							if i > 0 {
								b.WriteString(", ")
							}
							fmt.Fprintf(&b, "%q", m)
						}
						b.WriteString("},\n")
					}
					b.WriteString("\t\t\t\t},\n")
				}

				if model.Cost != nil {
					b.WriteString("\t\t\t\tCost: &ModelCost{\n")
					fmt.Fprintf(&b, "\t\t\t\t\tInput: %f,\n", model.Cost.Input)
					fmt.Fprintf(&b, "\t\t\t\t\tOutput: %f,\n", model.Cost.Output)
					if model.Cost.CacheRead > 0 {
						fmt.Fprintf(&b, "\t\t\t\t\tCacheRead: %f,\n", model.Cost.CacheRead)
					}
					if model.Cost.CacheWrite > 0 {
						fmt.Fprintf(&b, "\t\t\t\t\tCacheWrite: %f,\n", model.Cost.CacheWrite)
					}
					b.WriteString("\t\t\t\t},\n")
				}

				if model.Limit != nil {
					b.WriteString("\t\t\t\tLimit: &ModelLimit{\n")
					fmt.Fprintf(&b, "\t\t\t\t\tContext: %d,\n", model.Limit.Context)
					if model.Limit.Input > 0 {
						fmt.Fprintf(&b, "\t\t\t\t\tInput: %d,\n", model.Limit.Input)
					}
					if model.Limit.Output > 0 {
						fmt.Fprintf(&b, "\t\t\t\t\tOutput: %d,\n", model.Limit.Output)
					}
					b.WriteString("\t\t\t\t},\n")
				}

				if opts := emitReasoningOptions(model.ReasoningOptions); opts != "" {
					b.WriteString(opts)
				}

				b.WriteString("\t\t\t},\n")
			}

			b.WriteString("\t\t},\n")
		}

		b.WriteString("\t},\n")
	}

	b.WriteString("}\n")

	// Write to file
	if err := os.WriteFile(outputFile, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	fmt.Printf("Successfully generated %s\n", outputFile)
	return nil
}

// emitReasoningOptions renders a model's reasoning_options as a Go literal. It
// returns "" when there are no usable options. JSON nulls in effort value lists
// (which decode to "") are dropped, and Min/Max are emitted via the intPtr
// helper so nil bounds stay nil.
func emitReasoningOptions(opts []ReasoningOption) string {
	if len(opts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\t\t\t\tReasoningOptions: []ReasoningOption{\n")
	for _, ro := range opts {
		if ro.Type == "" {
			continue
		}
		b.WriteString("\t\t\t\t\t{")
		fmt.Fprintf(&b, "Type: %q", ro.Type)
		vals := make([]string, 0, len(ro.Values))
		for _, v := range ro.Values {
			if v != "" {
				vals = append(vals, v)
			}
		}
		if len(vals) > 0 {
			b.WriteString(", Values: []string{")
			for i, v := range vals {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q", v)
			}
			b.WriteString("}")
		}
		if ro.Min != nil {
			fmt.Fprintf(&b, ", Min: intPtr(%d)", *ro.Min)
		}
		if ro.Max != nil {
			fmt.Fprintf(&b, ", Max: intPtr(%d)", *ro.Max)
		}
		b.WriteString("},\n")
	}
	b.WriteString("\t\t\t\t},\n")
	return b.String()
}

func escapeString(s string) string {
	// Basic escaping for Go string literals
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
