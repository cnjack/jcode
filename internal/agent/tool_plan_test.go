package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type planTestTool struct {
	info *schema.ToolInfo
	err  error
}

func (t planTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, t.err
}

func testDescriptor(name string, exposure ToolExposure) ToolDescriptor {
	return ToolDescriptor{
		Tool:     planTestTool{info: &schema.ToolInfo{Name: name}},
		Name:     name,
		Exposure: exposure,
	}
}

func TestToolPlanBuilderClassifiesByContext(t *testing.T) {
	descriptors := []ToolDescriptor{
		func() ToolDescriptor {
			d := testDescriptor("computer_apps", ToolExposureDeferred)
			d.RequiredCapabilities = []string{"computer"}
			return d
		}(),
		func() ToolDescriptor {
			d := testDescriptor("ask_user", ToolExposureDirect)
			d.Transports = []string{ToolTransportWeb, ToolTransportTUI}
			return d
		}(),
		testDescriptor("internal_native", ToolExposureDirectModelOnly),
		func() ToolDescriptor {
			d := testDescriptor("edit", ToolExposureDirect)
			d.Modes = []string{ToolModeNormal}
			return d
		}(),
		func() ToolDescriptor {
			d := testDescriptor("browser_open", ToolExposureDeferred)
			d.Transports = []string{ToolTransportTUI, ToolTransportWeb}
			d.RequiredCapabilities = []string{"browser"}
			return d
		}(),
		testDescriptor("reviewer_only", ToolExposureHidden),
		testDescriptor("read", ToolExposureDirect),
	}

	tests := []struct {
		name      string
		context   ToolPlanContext
		direct    []string
		deferred  []string
		hidden    []string
		modelOnly []string
	}{
		{
			name: "tui normal with browser and computer",
			context: ToolPlanContext{
				Transport: ToolTransportTUI, Mode: ToolModeNormal,
				Capabilities: map[string]bool{"browser": true, "computer": true},
			},
			direct:    []string{"ask_user", "edit", "read"},
			deferred:  []string{"browser_open", "computer_apps"},
			hidden:    []string{"reviewer_only"},
			modelOnly: []string{"internal_native"},
		},
		{
			name: "web plan gates normal and missing capabilities",
			context: ToolPlanContext{
				Transport: ToolTransportWeb, Mode: ToolModePlan,
				Capabilities: map[string]bool{"browser": true},
			},
			direct:    []string{"ask_user", "read"},
			deferred:  []string{"browser_open"},
			hidden:    []string{"computer_apps", "edit", "reviewer_only"},
			modelOnly: []string{"internal_native"},
		},
		{
			name: "acp excludes interactive and browser tools",
			context: ToolPlanContext{
				Transport: ToolTransportACP, Mode: ToolModeNormal,
				Capabilities: map[string]bool{"browser": true, "computer": true},
			},
			direct:    []string{"edit", "read"},
			deferred:  []string{"computer_apps"},
			hidden:    []string{"ask_user", "browser_open", "reviewer_only"},
			modelOnly: []string{"internal_native"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := NewToolPlanBuilder(descriptors).Build(context.Background(), tt.context)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			assertDescriptorNames(t, "direct", plan.Direct, tt.direct)
			assertDescriptorNames(t, "deferred", plan.Deferred, tt.deferred)
			assertDescriptorNames(t, "hidden", plan.Hidden, tt.hidden)
			assertDescriptorNames(t, "direct model only", plan.DirectModelOnly, tt.modelOnly)
		})
	}
}

func TestToolPlanBuilderSortsDescriptorsAndMetadata(t *testing.T) {
	zeta := testDescriptor("zeta", ToolExposureDirect)
	zeta.Aliases = []string{"z", "beta"}
	zeta.Transports = []string{"web", "tui"}
	zeta.Modes = []string{"plan", "normal"}
	zeta.RequiredCapabilities = []string{"second", "first"}

	plan, err := NewToolPlanBuilder([]ToolDescriptor{
		zeta,
		testDescriptor("alpha", ToolExposureDirect),
		testDescriptor("middle", ToolExposureDeferred),
	}).Build(context.Background(), ToolPlanContext{
		Transport: ToolTransportTUI,
		Mode:      ToolModeNormal,
		Capabilities: map[string]bool{
			"first": true, "second": true,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	assertDescriptorNames(t, "direct", plan.Direct, []string{"alpha", "zeta"})
	assertDescriptorNames(t, "deferred", plan.Deferred, []string{"middle"})

	got := plan.Direct[1]
	if !reflect.DeepEqual(got.Aliases, []string{"beta", "z"}) {
		t.Fatalf("aliases = %v, want sorted aliases", got.Aliases)
	}
	if !reflect.DeepEqual(got.Transports, []string{"tui", "web"}) {
		t.Fatalf("transports = %v, want stable order", got.Transports)
	}
	if !reflect.DeepEqual(got.Modes, []string{"normal", "plan"}) {
		t.Fatalf("modes = %v, want stable order", got.Modes)
	}
	if !reflect.DeepEqual(got.RequiredCapabilities, []string{"first", "second"}) {
		t.Fatalf("required capabilities = %v, want stable order", got.RequiredCapabilities)
	}
}

func TestToolPlanToolViewsAreStableCopies(t *testing.T) {
	plan, err := NewToolPlanBuilder([]ToolDescriptor{
		testDescriptor("delta", ToolExposureHidden),
		testDescriptor("beta", ToolExposureDeferred),
		testDescriptor("gamma", ToolExposureDirectModelOnly),
		testDescriptor("alpha", ToolExposureDirect),
	}).Build(context.Background(), ToolPlanContext{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	assertToolNames(t, "direct tools", plan.DirectTools(), []string{"alpha"})
	assertToolNames(t, "deferred tools", plan.DeferredTools(), []string{"beta"})
	assertToolNames(t, "all runtime tools", plan.AllRuntimeTools(),
		[]string{"alpha", "beta", "gamma"})

	direct := plan.DirectTools()
	direct[0] = nil
	if fresh := plan.DirectTools(); len(fresh) != 1 || fresh[0] == nil {
		t.Fatalf("DirectTools() returned shared slice: %v", fresh)
	}
	runtimeTools := plan.AllRuntimeTools()
	runtimeTools[0] = nil
	if fresh := plan.AllRuntimeTools(); len(fresh) != 3 || fresh[0] == nil {
		t.Fatalf("AllRuntimeTools() returned shared slice: %v", fresh)
	}
}

func TestToolPlanBuilderRejectsInvalidDescriptors(t *testing.T) {
	infoErr := errors.New("info unavailable")
	tests := []struct {
		name        string
		descriptors []ToolDescriptor
		wantError   string
	}{
		{
			name: "nil tool",
			descriptors: []ToolDescriptor{{
				Name: "missing", Exposure: ToolExposureDirect,
			}},
			wantError: "nil tool",
		},
		{
			name: "tool info error",
			descriptors: []ToolDescriptor{{
				Tool: planTestTool{err: infoErr}, Name: "broken", Exposure: ToolExposureDirect,
			}},
			wantError: "info unavailable",
		},
		{
			name: "nil tool info",
			descriptors: []ToolDescriptor{{
				Tool: planTestTool{}, Name: "empty", Exposure: ToolExposureDirect,
			}},
			wantError: "nil ToolInfo",
		},
		{
			name: "empty tool info name",
			descriptors: []ToolDescriptor{{
				Tool: planTestTool{info: &schema.ToolInfo{}}, Name: "empty", Exposure: ToolExposureDirect,
			}},
			wantError: "ToolInfo with empty name",
		},
		{
			name: "descriptor and info mismatch",
			descriptors: []ToolDescriptor{{
				Tool: planTestTool{info: &schema.ToolInfo{Name: "actual"}}, Name: "declared", Exposure: ToolExposureDirect,
			}},
			wantError: "does not match",
		},
		{
			name: "duplicate name across exposures",
			descriptors: []ToolDescriptor{
				testDescriptor("same", ToolExposureDirect),
				testDescriptor("same", ToolExposureDeferred),
			},
			wantError: "duplicate tool name",
		},
		{
			name:        "reserved canonical name",
			descriptors: []ToolDescriptor{testDescriptor(ToolSearchReservedName, ToolExposureDirect)},
			wantError:   "reserved",
		},
		{
			name: "reserved alias",
			descriptors: []ToolDescriptor{func() ToolDescriptor {
				d := testDescriptor("finder", ToolExposureDeferred)
				d.Aliases = []string{ToolSearchReservedName}
				return d
			}()},
			wantError: "reserved",
		},
		{
			name: "shared alias",
			descriptors: []ToolDescriptor{
				func() ToolDescriptor {
					d := testDescriptor("first", ToolExposureDirect)
					d.Aliases = []string{"common"}
					return d
				}(),
				func() ToolDescriptor {
					d := testDescriptor("second", ToolExposureDeferred)
					d.Aliases = []string{"common"}
					return d
				}(),
			},
			wantError: "search key",
		},
		{
			name: "invalid exposure",
			descriptors: []ToolDescriptor{
				testDescriptor("invalid", ToolExposure("sometimes")),
			},
			wantError: "invalid exposure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewToolPlanBuilder(tt.descriptors).Build(context.Background(), ToolPlanContext{})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Build() error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestToolPlanValidateRejectsPartitionOverlap(t *testing.T) {
	descriptor := testDescriptor("read", ToolExposureDirect)
	plan := ToolPlan{
		Direct:   []ToolDescriptor{descriptor},
		Deferred: []ToolDescriptor{descriptor},
	}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "both direct and deferred") {
		t.Fatalf("Validate() error = %v, want direct/deferred overlap", err)
	}
}

func assertDescriptorNames(t *testing.T, label string, descriptors []ToolDescriptor, want []string) {
	t.Helper()
	got := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		got[i] = descriptor.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s names = %v, want %v", label, got, want)
	}
}

func assertToolNames(t *testing.T, label string, tools []tool.BaseTool, want []string) {
	t.Helper()
	got := make([]string, len(tools))
	for i, candidate := range tools {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatalf("%s tool %d Info() error = %v", label, i, err)
		}
		got[i] = info.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s names = %v, want %v", label, got, want)
	}
}

var _ tool.BaseTool = planTestTool{}
