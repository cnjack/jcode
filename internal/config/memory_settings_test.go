package config

import (
	"sync"
	"testing"
)

func memoryBool(v bool) *bool { return &v }

func TestMemorySettingsDefaults(t *testing.T) {
	for _, cfg := range []*Config{nil, {}} {
		if !MemoryEnabled(cfg) {
			t.Fatal("memory must default enabled")
		}
		if !MemoryGenerate(cfg) {
			t.Fatal("memory generation must default enabled")
		}
		if got := MemoryDailyTokenBudget(cfg); got != 300000 {
			t.Fatalf("daily token budget = %d, want 300000", got)
		}
		if got := MemoryCooldownHours(cfg); got != 6 {
			t.Fatalf("cooldown = %d, want 6", got)
		}
		if got := MemoryMaxAgeDays(cfg); got != 30 {
			t.Fatalf("max age = %d, want 30", got)
		}
		if got := MemoryMaxUnusedDays(cfg); got != 45 {
			t.Fatalf("max unused = %d, want 45", got)
		}
		if got := MemoryPhase2TopN(cfg); got != 40 {
			t.Fatalf("phase2 top N = %d, want 40", got)
		}
		if got := MemorySummaryInjectTokens(cfg); got != 1200 {
			t.Fatalf("summary inject tokens = %d, want 1200", got)
		}
	}
}

func TestMemorySettingsAreDetached(t *testing.T) {
	enabled, generate := true, true
	input := &MemoryConfig{
		Enabled: &enabled, Generate: &generate, Model: "provider/model",
		DailyTokenBudget: 42, CooldownHours: 3,
	}
	cfg := &Config{Model: "main/model"}
	if cfg.MemoryConfigSnapshot() != nil {
		t.Fatal("absent Memory block did not remain distinguishable")
	}
	cfg.SetMemory(input)

	enabled = false
	generate = false
	input.Model = "mutated/model"
	got := cfg.MemorySettings()
	if !*got.Enabled || !*got.Generate || got.Model != "provider/model" {
		t.Fatalf("SetMemory retained caller-owned state: %+v", got)
	}

	*got.Enabled = false
	got.Model = "snapshot/mutated"
	if !MemoryEnabled(cfg) || cfg.MemorySettings().Model != "provider/model" {
		t.Fatal("MemorySettings exposed live state")
	}

	pipeline := cfg.MemoryPipelineSnapshot()
	cfg.SetMemory(&MemoryConfig{Enabled: memoryBool(false), Model: "new/model"})
	if !MemoryEnabled(pipeline) || pipeline.MemorySettings().Model != "provider/model" {
		t.Fatal("pipeline snapshot changed after live settings publication")
	}
	if pipeline.Memory == cfg.Memory {
		t.Fatal("pipeline snapshot retained live Memory pointer")
	}
	stored := cfg.MemoryConfigSnapshot()
	*stored.Enabled = true
	stored.Model = "rollback/mutated"
	if MemoryEnabled(cfg) || cfg.MemorySettings().Model != "new/model" {
		t.Fatal("MemoryConfigSnapshot exposed live state")
	}

	cfg.SetMemory(nil)
	if !MemoryEnabled(cfg) || !MemoryGenerate(cfg) {
		t.Fatal("nil Memory block did not restore enabled defaults")
	}
}

func TestMemoryGenerateRequiresEnabled(t *testing.T) {
	cfg := &Config{}
	cfg.SetMemory(&MemoryConfig{Enabled: memoryBool(false), Generate: memoryBool(true)})
	if MemoryGenerate(cfg) {
		t.Fatal("generation enabled while memory is disabled")
	}
	if !MemoryGenerateSetting(cfg) {
		t.Fatal("master switch erased the stored generation preference")
	}
}

func TestMemorySettingsConcurrentPublishRead(t *testing.T) {
	cfg := &Config{}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func(value bool) {
			defer wg.Done()
			for range 500 {
				cfg.SetMemory(&MemoryConfig{
					Enabled: &value, Generate: &value, DailyTokenBudget: 123,
				})
			}
		}(i%2 == 0)
		go func() {
			defer wg.Done()
			for range 500 {
				_ = cfg.MemorySettings()
				_ = cfg.MemoryPipelineSnapshot()
				_ = MemoryEnabled(cfg)
				_ = MemoryGenerate(cfg)
				_ = MemoryDailyTokenBudget(cfg)
			}
		}()
	}
	wg.Wait()
}
