package browser

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestManagedChromeEnvUsesSystemHomeOnlyOnDarwin(t *testing.T) {
	original := []string{
		"HOME=/isolated/eval-home",
		"PATH=/usr/bin:/bin",
		"JCODE_WEB_TOKEN=must-not-reach-chrome",
		"LANG=en_US.UTF-8",
	}

	got := managedChromeEnv("darwin", original, "/Users/account")
	if !slices.Contains(got, "HOME=/Users/account") {
		t.Fatalf("managed Chrome HOME was not replaced: %v", got)
	}
	for _, forbidden := range []string{"HOME=/isolated/eval-home", "JCODE_WEB_TOKEN=must-not-reach-chrome"} {
		if slices.Contains(got, forbidden) {
			t.Fatalf("managed Chrome environment contains forbidden entry %q", forbidden)
		}
	}
	for _, preserved := range []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"} {
		if !slices.Contains(got, preserved) {
			t.Fatalf("managed Chrome environment lost %q: %v", preserved, got)
		}
	}
	if !slices.Equal(original, []string{
		"HOME=/isolated/eval-home",
		"PATH=/usr/bin:/bin",
		"JCODE_WEB_TOKEN=must-not-reach-chrome",
		"LANG=en_US.UTF-8",
	}) {
		t.Fatal("managedChromeEnv mutated its input")
	}
}

func TestExistingHomeDirRequiresAnExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(file, []byte("not a home"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := existingHomeDir(dir); got != dir {
		t.Fatalf("existingHomeDir(directory) = %q, want directory", got)
	}
	for _, invalid := range []string{
		"",
		"relative/home",
		" " + dir,
		file,
		filepath.Join(dir, "missing"),
	} {
		if got := existingHomeDir(invalid); got != "" {
			t.Errorf("existingHomeDir(%q) = %q, want empty", invalid, got)
		}
	}
}

func TestManagedChromeEnvRetainsHomeForOtherPlatformsOrInvalidAccountHome(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		systemHome string
	}{
		{name: "non darwin", goos: "linux", systemHome: "/home/account"},
		{name: "empty account home", goos: "darwin", systemHome: ""},
		{name: "relative account home", goos: "darwin", systemHome: "relative/home"},
		{name: "whitespace account home", goos: "darwin", systemHome: " /Users/account"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := managedChromeEnv(tt.goos, []string{
				"HOME=/isolated/eval-home",
				"JCODE_WEB_TOKEN=must-not-reach-chrome",
			}, tt.systemHome)
			if !slices.Contains(got, "HOME=/isolated/eval-home") {
				t.Fatalf("HOME changed for %s: %v", tt.name, got)
			}
			for _, entry := range got {
				if entry == "JCODE_WEB_TOKEN=must-not-reach-chrome" {
					t.Fatal("JCODE_WEB_TOKEN reached managed Chrome")
				}
			}
		})
	}
}
