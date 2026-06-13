package theme

import (
	"reflect"
	"regexp"
	"testing"
)

var hexRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// metaFields are Theme fields that are not colors and so are exempt from the
// hex-format check.
var metaFields = map[string]bool{
	"Name": true, "DisplayName": true, "Appearance": true,
}

// TestThemesComplete asserts every built-in theme fully populates every color
// token with a valid #RRGGBB value. A missing token would silently fall
// through to another theme's value (web) or render black (TUI), so this is the
// guard that keeps the single source of truth honest.
func TestThemesComplete(t *testing.T) {
	for _, th := range All() {
		if th.Name == "" || th.DisplayName == "" {
			t.Errorf("theme %+v: missing Name/DisplayName", th)
		}
		if th.Appearance != Dark && th.Appearance != Light {
			t.Errorf("theme %q: invalid appearance %q", th.Name, th.Appearance)
		}
		v := reflect.ValueOf(th)
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			name := typ.Field(i).Name
			if metaFields[name] {
				continue
			}
			val := v.Field(i).String()
			if !hexRe.MatchString(val) {
				t.Errorf("theme %q: field %s = %q is not a valid #RRGGBB hex", th.Name, name, val)
			}
		}
	}
}

// TestThemeNamesUnique ensures no two built-ins share a name (which would make
// one unreachable via Get and produce a duplicate data-theme CSS block).
func TestThemeNamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range All() {
		if seen[th.Name] {
			t.Errorf("duplicate theme name %q", th.Name)
		}
		seen[th.Name] = true
	}
}

// TestDefaultsResolvable ensures the named defaults exist and have the expected
// appearance, since startup falls back to them.
func TestDefaultsResolvable(t *testing.T) {
	if d, ok := Get(DefaultDark); !ok || d.Appearance != Dark {
		t.Errorf("DefaultDark %q must exist and be dark", DefaultDark)
	}
	if l, ok := Get(DefaultLight); !ok || l.Appearance != Light {
		t.Errorf("DefaultLight %q must exist and be light", DefaultLight)
	}
	if Default(Dark) != DefaultDark || Default(Light) != DefaultLight {
		t.Error("Default() does not match the Default* constants")
	}
}

// TestResolveFallback ensures unknown/empty names degrade to the dark default
// rather than returning a zero-value (all-black) theme.
func TestResolveFallback(t *testing.T) {
	for _, name := range []string{"", "does-not-exist"} {
		if got := Resolve(name); got.Name != DefaultDark {
			t.Errorf("Resolve(%q) = %q, want %q", name, got.Name, DefaultDark)
		}
	}
	if got := Resolve("nord-dark"); got.Name != "nord-dark" {
		t.Errorf("Resolve(known) returned %q", got.Name)
	}
}

// TestCSSVariablesPopulated ensures the generated CSS will never emit an empty
// value and that the legacy variable names the web app already depends on are
// all present.
func TestCSSVariablesPopulated(t *testing.T) {
	required := []string{
		"--color-primary", "--color-background", "--color-surface",
		"--color-foreground", "--color-muted-foreground", "--color-border",
		"--color-success-fg", "--color-error-fg", "--color-warning-fg",
		"--color-info-fg", "--color-destructive", "--color-sidebar-bg",
	}
	for _, th := range All() {
		present := map[string]bool{}
		for _, cv := range th.CSSVariables() {
			if cv.Value == "" {
				t.Errorf("theme %q: %s has empty value", th.Name, cv.Name)
			}
			present[cv.Name] = true
		}
		for _, name := range required {
			if !present[name] {
				t.Errorf("theme %q: missing required CSS var %s", th.Name, name)
			}
		}
	}
}
