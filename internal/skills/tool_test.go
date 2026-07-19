package skills

import (
	"context"
	"strings"
	"testing"
)

func TestLoadSkillToolSchemaDoesNotEmbedDynamicSkillList(t *testing.T) {
	loader := &Loader{skills: map[string]*Skill{
		"dynamic-only-sentinel": {
			Name:        "dynamic-only-sentinel",
			Description: "changes at runtime",
			Body:        "instructions",
		},
	}}
	loadSkill := NewLoadSkillTool(loader)
	first, err := loadSkill.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Desc, "dynamic-only-sentinel") || strings.Contains(first.Desc, "changes at runtime") {
		t.Fatalf("load_skill schema embeds the dynamic loader catalog: %q", first.Desc)
	}

	loader.skills["later-skill"] = &Skill{Name: "later-skill", Description: "added later"}
	second, err := loadSkill.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Desc != first.Desc {
		t.Fatalf("load_skill schema changed with loader contents: before=%q after=%q", first.Desc, second.Desc)
	}
}

func TestLoadSkillToolUnknownAndMissingNamesListAvailableSkills(t *testing.T) {
	loader := &Loader{skills: map[string]*Skill{
		"available-sentinel": {
			Name:        "available-sentinel",
			Description: "runtime catalog entry",
			Body:        "instructions",
		},
	}}
	loadSkill := NewLoadSkillTool(loader)
	for _, args := range []string{`{"name":"unknown"}`, `{}`} {
		result, err := loadSkill.InvokableRun(context.Background(), args)
		if err != nil {
			t.Fatalf("InvokableRun(%s) error = %v", args, err)
		}
		if !strings.Contains(result, "Available skills:") || !strings.Contains(result, "available-sentinel") {
			t.Fatalf("InvokableRun(%s) result does not list runtime skills: %q", args, result)
		}
	}
}
