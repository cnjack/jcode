package config

import (
	"reflect"
	"testing"
)

func TestModelStateForgetProviderDropsOnlyThatProvider(t *testing.T) {
	keep := ModelRef{Provider: "openai", Model: "gpt-5"}
	gone := ModelRef{Provider: "Share Copilot", Model: "gpt-6-sol"}
	state := &ModelState{
		Recent:         []ModelRef{gone, keep},
		Favorite:       []ModelRef{gone},
		EnabledModels:  []ModelRef{keep, gone},
		DisabledModels: []ModelRef{{Provider: "Share Copilot", Model: "old"}},
		EffortOverrides: map[string]string{
			"Share Copilot/gpt-6-sol": "high",
			"openai/gpt-5":            "low",
			"Share Copilot2/x":        "max",
		},
	}
	if !state.ForgetProvider("Share Copilot") {
		t.Fatal("ForgetProvider reported no change")
	}
	want := &ModelState{
		Recent:          []ModelRef{keep},
		Favorite:        []ModelRef{},
		EnabledModels:   []ModelRef{keep},
		DisabledModels:  []ModelRef{},
		EffortOverrides: map[string]string{"openai/gpt-5": "low", "Share Copilot2/x": "max"},
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("state = %#v, want %#v", state, want)
	}
	if state.ForgetProvider("Share Copilot") {
		t.Fatal("second ForgetProvider should be a no-op")
	}
}
