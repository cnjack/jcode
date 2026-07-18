package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

const (
	ToolSearchReservedName = "tool_search"

	ToolTransportTUI = "tui"
	ToolTransportWeb = "web"
	ToolTransportACP = "acp"

	ToolModeNormal = "normal"
	ToolModePlan   = "plan"
)

// ToolExposure controls when a registered tool is disclosed to the model.
type ToolExposure string

const (
	ToolExposureDirect          ToolExposure = "direct"
	ToolExposureDeferred        ToolExposure = "deferred"
	ToolExposureHidden          ToolExposure = "hidden"
	ToolExposureDirectModelOnly ToolExposure = "direct_model_only"
)

// ToolDescriptor is the transport-independent metadata for one executable tool.
// Empty transport/mode lists mean the tool is available in every transport/mode.
// RequiredCapabilities uses all-of semantics.
type ToolDescriptor struct {
	Tool                 tool.BaseTool
	Name                 string
	Aliases              []string
	Source               string
	Bundle               string
	Exposure             ToolExposure
	Transports           []string
	Modes                []string
	RequiredCapabilities []string
	ApprovalClass        string
}

// ToolPlanContext identifies the runtime surface for which a plan is built.
type ToolPlanContext struct {
	Transport    string
	Mode         string
	Capabilities map[string]bool
}

// ToolPlan is the effective, stably ordered exposure plan for one context.
type ToolPlan struct {
	Direct          []ToolDescriptor
	Deferred        []ToolDescriptor
	Hidden          []ToolDescriptor
	DirectModelOnly []ToolDescriptor
}

// DirectTools returns a new slice containing the tools disclosed initially.
func (p ToolPlan) DirectTools() []tool.BaseTool {
	return toolsFromDescriptors(p.Direct)
}

// DeferredTools returns a new slice containing the tools available to search.
func (p ToolPlan) DeferredTools() []tool.BaseTool {
	return toolsFromDescriptors(p.Deferred)
}

// AllRuntimeTools returns every executable endpoint in stable name order.
// Hidden tools are deliberately excluded: a transport, mode, or capability
// gate must be an execution boundary, not merely a schema-visibility hint.
func (p ToolPlan) AllRuntimeTools() []tool.BaseTool {
	descriptors := make([]ToolDescriptor, 0,
		len(p.Direct)+len(p.Deferred)+len(p.DirectModelOnly))
	descriptors = append(descriptors, p.Direct...)
	descriptors = append(descriptors, p.Deferred...)
	descriptors = append(descriptors, p.DirectModelOnly...)
	sortDescriptors(descriptors)
	return toolsFromDescriptors(descriptors)
}

// Validate checks that no effective plan partition contains the same tool name.
func (p ToolPlan) Validate() error {
	seen := make(map[string]string)
	groups := []struct {
		name  string
		tools []ToolDescriptor
	}{
		{name: "direct", tools: p.Direct},
		{name: "deferred", tools: p.Deferred},
		{name: "hidden", tools: p.Hidden},
		{name: "direct_model_only", tools: p.DirectModelOnly},
	}
	for _, group := range groups {
		for _, descriptor := range group.tools {
			name := strings.TrimSpace(descriptor.Name)
			if previous, ok := seen[name]; ok {
				return fmt.Errorf("tool plan: tool %q appears in both %s and %s", name, previous, group.name)
			}
			seen[name] = group.name
		}
	}
	return nil
}

// ToolPlanBuilder validates descriptors and classifies them for a runtime context.
type ToolPlanBuilder struct {
	descriptors []ToolDescriptor
}

func NewToolPlanBuilder(descriptors []ToolDescriptor) *ToolPlanBuilder {
	return &ToolPlanBuilder{descriptors: append([]ToolDescriptor(nil), descriptors...)}
}

// Build returns an effective plan. A descriptor that fails a transport, mode, or
// capability gate is retained in Hidden so callers can inspect why it was omitted.
func (b *ToolPlanBuilder) Build(ctx context.Context, planContext ToolPlanContext) (ToolPlan, error) {
	var plan ToolPlan
	if b == nil {
		return plan, fmt.Errorf("tool plan: nil builder")
	}

	descriptors, err := validateDescriptors(ctx, b.descriptors)
	if err != nil {
		return plan, err
	}

	for _, descriptor := range descriptors {
		effective := descriptor
		if !descriptorEnabled(descriptor, planContext) {
			effective.Exposure = ToolExposureHidden
		}
		switch effective.Exposure {
		case ToolExposureDirect:
			plan.Direct = append(plan.Direct, effective)
		case ToolExposureDeferred:
			plan.Deferred = append(plan.Deferred, effective)
		case ToolExposureHidden:
			plan.Hidden = append(plan.Hidden, effective)
		case ToolExposureDirectModelOnly:
			plan.DirectModelOnly = append(plan.DirectModelOnly, effective)
		default:
			return ToolPlan{}, fmt.Errorf("tool plan: tool %q has invalid exposure %q", effective.Name, effective.Exposure)
		}
	}

	sortDescriptors(plan.Direct)
	sortDescriptors(plan.Deferred)
	sortDescriptors(plan.Hidden)
	sortDescriptors(plan.DirectModelOnly)
	if err := plan.Validate(); err != nil {
		return ToolPlan{}, err
	}
	return plan, nil
}

func validateDescriptors(ctx context.Context, input []ToolDescriptor) ([]ToolDescriptor, error) {
	result := make([]ToolDescriptor, 0, len(input))
	names := make(map[string]struct{}, len(input))
	searchKeys := make(map[string]string, len(input))
	for i, original := range input {
		descriptor := cloneDescriptor(original)
		descriptor.Name = strings.TrimSpace(descriptor.Name)
		if descriptor.Tool == nil {
			return nil, fmt.Errorf("tool plan: descriptor %d has nil tool", i)
		}
		info, err := descriptor.Tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool plan: read info for descriptor %d: %w", i, err)
		}
		if info == nil {
			return nil, fmt.Errorf("tool plan: descriptor %d returned nil ToolInfo", i)
		}
		infoName := strings.TrimSpace(info.Name)
		if infoName == "" {
			return nil, fmt.Errorf("tool plan: descriptor %d returned ToolInfo with empty name", i)
		}
		if descriptor.Name == "" {
			return nil, fmt.Errorf("tool plan: descriptor %d has empty name", i)
		}
		if descriptor.Name != infoName {
			return nil, fmt.Errorf("tool plan: descriptor name %q does not match ToolInfo name %q", descriptor.Name, infoName)
		}
		if !validExposure(descriptor.Exposure) {
			return nil, fmt.Errorf("tool plan: tool %q has invalid exposure %q", descriptor.Name, descriptor.Exposure)
		}
		if descriptor.Name == ToolSearchReservedName {
			return nil, fmt.Errorf("tool plan: name %q is reserved", ToolSearchReservedName)
		}
		if _, exists := names[descriptor.Name]; exists {
			return nil, fmt.Errorf("tool plan: duplicate tool name %q", descriptor.Name)
		}
		names[descriptor.Name] = struct{}{}

		keys := append([]string{descriptor.Name}, descriptor.Aliases...)
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				return nil, fmt.Errorf("tool plan: tool %q has empty alias", descriptor.Name)
			}
			if key == ToolSearchReservedName {
				return nil, fmt.Errorf("tool plan: search key %q is reserved", ToolSearchReservedName)
			}
			if owner, exists := searchKeys[key]; exists {
				return nil, fmt.Errorf("tool plan: search key %q is shared by tools %q and %q", key, owner, descriptor.Name)
			}
			searchKeys[key] = descriptor.Name
		}
		result = append(result, descriptor)
	}
	return result, nil
}

func cloneDescriptor(descriptor ToolDescriptor) ToolDescriptor {
	descriptor.Aliases = normalizedStrings(descriptor.Aliases)
	descriptor.Transports = normalizedStrings(descriptor.Transports)
	descriptor.Modes = normalizedStrings(descriptor.Modes)
	descriptor.RequiredCapabilities = normalizedStrings(descriptor.RequiredCapabilities)
	return descriptor
}

func normalizedStrings(values []string) []string {
	if values == nil {
		return nil
	}
	result := append([]string(nil), values...)
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	sort.Strings(result)
	return result
}

func validExposure(exposure ToolExposure) bool {
	switch exposure {
	case ToolExposureDirect, ToolExposureDeferred, ToolExposureHidden, ToolExposureDirectModelOnly:
		return true
	default:
		return false
	}
}

func descriptorEnabled(descriptor ToolDescriptor, planContext ToolPlanContext) bool {
	if !matchesScope(descriptor.Transports, strings.TrimSpace(planContext.Transport)) ||
		!matchesScope(descriptor.Modes, strings.TrimSpace(planContext.Mode)) {
		return false
	}
	for _, capability := range descriptor.RequiredCapabilities {
		if !planContext.Capabilities[capability] {
			return false
		}
	}
	return true
}

func matchesScope(allowed []string, current string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == current {
			return true
		}
	}
	return false
}

func sortDescriptors(descriptors []ToolDescriptor) {
	sort.SliceStable(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	})
}

func toolsFromDescriptors(descriptors []ToolDescriptor) []tool.BaseTool {
	result := make([]tool.BaseTool, len(descriptors))
	for i, descriptor := range descriptors {
		result[i] = descriptor.Tool
	}
	return result
}
