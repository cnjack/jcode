// Package theme is the single source of truth for jcode's built-in color
// themes. The same Theme structs drive lipgloss colors in the terminal UI
// (internal/tui) and CSS custom properties in the web frontend.
//
// Web consumption is via generated CSS: `go generate ./internal/theme/...`
// (see gen/) writes web/src/styles/tokens.generated.css with one
// html[data-theme="<name>"] block per theme, so the two renderers can never
// drift — the web tokens are a pure function of this file.
package theme

//go:generate go run ./gen

// Appearance categorizes a theme as light or dark. It selects the markdown
// (glamour) style in the TUI and the meta theme-color / .dark class in web.
type Appearance string

const (
	Dark  Appearance = "dark"
	Light Appearance = "light"
)

// Built-in defaults for each appearance. Used as the startup fallback when no
// theme is persisted, picked by terminal-background detection.
const (
	DefaultDark  = "jcode-dark"
	DefaultLight = "jcode-light"
)

// Theme is a complete semantic color palette. Every field is a #RRGGBB hex
// string. Tokens are semantic (their role), never positional, so a theme is
// just a different set of values for the same roles.
type Theme struct {
	Name        string
	DisplayName string
	Appearance  Appearance

	// Brand / accent
	Primary     string // brand accent: assistant label, logo, focused chrome
	OnPrimary   string // text/icons on a Primary fill (prompt pill, focused button)
	Accent      string // secondary accent (cyan family): tool labels, spinner
	BorderFocus string // focused/active border (defaults to Primary)

	// Surfaces & text
	Bg           string // root application background
	Surface      string // elevated panel/card/dialog/sidebar background
	MutedSurface string // muted/secondary surface (badges, disabled rows, bars)
	SelectionBg  string // selection / active-row highlight background
	Text         string // primary foreground text
	TextDim      string // secondary/dim text
	TextMuted    string // lowest-emphasis text: section titles, separators, args
	Border       string // default borders, dividers, rules

	// Semantic foregrounds + subtle background washes
	Success     string
	Warning     string
	Error       string
	Info        string
	SuccessBg   string // green wash (also diff-added background)
	WarningBg   string
	ErrorBg     string // red wash (also diff-removed background)
	InfoBg      string
	Destructive string // vivid destructive action (delete buttons)

	// Special roles
	PlanMode string // plan-mode indicator (soft purple)
	Subagent string // subagent accent (purple)
	RoleUser string // user turn accent

	// Diff foregrounds (brighter than Success/Error)
	DiffAddedFg   string
	DiffRemovedFg string
}

// IsDark reports whether the theme is dark-appearance.
func (t Theme) IsDark() bool { return t.Appearance == Dark }

// GlamourStyle returns the glamour standard-style name for markdown rendering.
func (t Theme) GlamourStyle() string {
	if t.Appearance == Light {
		return "light"
	}
	return "dark"
}

// CSSVar is a single CSS custom property name/value pair.
type CSSVar struct {
	Name  string
	Value string
}

// CSSVariables returns the theme's web CSS custom properties in a stable
// order (so generated output is deterministic for drift checks). The variable
// names match those already consumed by the web app, plus a few new ones for
// tokens that had no prior web equivalent (--color-accent, --color-on-primary,
// --color-border-focus, --color-plan-mode, --color-subagent, --color-selection,
// --color-role-user, --color-text-muted, --color-diff-*).
func (t Theme) CSSVariables() []CSSVar {
	return []CSSVar{
		{"--color-primary", t.Primary},
		{"--color-on-primary", t.OnPrimary},
		{"--color-accent", t.Accent},
		{"--color-border-focus", t.BorderFocus},
		{"--color-background", t.Bg},
		{"--color-surface", t.Surface},
		{"--color-sidebar-bg", t.Surface},
		{"--color-muted", t.MutedSurface},
		{"--color-secondary", t.MutedSurface},
		{"--color-secondary-foreground", t.Text},
		{"--color-selection", t.SelectionBg},
		{"--color-foreground", t.Text},
		{"--color-muted-foreground", t.TextDim},
		{"--color-text-muted", t.TextMuted},
		{"--color-border", t.Border},
		{"--color-success-fg", t.Success},
		{"--color-success-bg", t.SuccessBg},
		{"--color-warning-fg", t.Warning},
		{"--color-warning-bg", t.WarningBg},
		{"--color-error-fg", t.Error},
		{"--color-error-bg", t.ErrorBg},
		{"--color-info-fg", t.Info},
		{"--color-info-bg", t.InfoBg},
		{"--color-destructive", t.Destructive},
		{"--color-plan-mode", t.PlanMode},
		{"--color-subagent", t.Subagent},
		{"--color-role-user", t.RoleUser},
		{"--color-diff-added-fg", t.DiffAddedFg},
		{"--color-diff-removed-fg", t.DiffRemovedFg},
	}
}

// builtins is the ordered list of built-in themes (darks first, then lights).
// Order is preserved by All() and the theme picker. jcode-dark/light keep the
// brand orange as Primary; third-party themes use their native accent.
var builtins = []Theme{
	{
		Name: "jcode-dark", DisplayName: "jcode Dark", Appearance: Dark,
		Primary: "#FF8400", OnPrimary: "#1A1A2E", Accent: "#06B6D4", BorderFocus: "#FF8400",
		Bg: "#111827", Surface: "#1A2333", MutedSurface: "#1F2A3C", SelectionBg: "#243047",
		Text: "#E5E7EB", TextDim: "#9CA3AF", TextMuted: "#6B7280", Border: "#374151",
		Success: "#10B981", Warning: "#F59E0B", Error: "#EF4444", Info: "#38BDF8",
		SuccessBg: "#0E2A20", WarningBg: "#2A1F0E", ErrorBg: "#2E1414", InfoBg: "#14202E", Destructive: "#F87171",
		PlanMode: "#C084FC", Subagent: "#8B5CF6", RoleUser: "#06B6D4",
		DiffAddedFg: "#34D399", DiffRemovedFg: "#F87171",
	},
	{
		Name: "midnight", DisplayName: "Midnight", Appearance: Dark,
		Primary: "#FF8400", OnPrimary: "#0A0E18", Accent: "#22D3EE", BorderFocus: "#FF8400",
		Bg: "#0A0E18", Surface: "#121828", MutedSurface: "#1B2540", SelectionBg: "#1B2540",
		Text: "#E6EAF2", TextDim: "#9AA4BC", TextMuted: "#5C6680", Border: "#283149",
		Success: "#34D399", Warning: "#FBBF24", Error: "#F87171", Info: "#60A5FA",
		SuccessBg: "#0B2620", WarningBg: "#2A2310", ErrorBg: "#2A1218", InfoBg: "#10202E", Destructive: "#FB7185",
		PlanMode: "#C4B5FD", Subagent: "#A78BFA", RoleUser: "#22D3EE",
		DiffAddedFg: "#4ADE80", DiffRemovedFg: "#FB7185",
	},
	{
		Name: "dracula", DisplayName: "Dracula", Appearance: Dark,
		Primary: "#FFB86C", OnPrimary: "#282A36", Accent: "#8BE9FD", BorderFocus: "#FFB86C",
		Bg: "#282A36", Surface: "#343746", MutedSurface: "#44475A", SelectionBg: "#44475A",
		Text: "#F8F8F2", TextDim: "#C7C9D1", TextMuted: "#6272A4", Border: "#44475A",
		Success: "#50FA7B", Warning: "#F1FA8C", Error: "#FF5555", Info: "#8BE9FD",
		SuccessBg: "#22372B", WarningBg: "#3A3A1F", ErrorBg: "#3A2230", InfoBg: "#1F3A3D", Destructive: "#FF5555",
		PlanMode: "#BD93F9", Subagent: "#BD93F9", RoleUser: "#8BE9FD",
		DiffAddedFg: "#50FA7B", DiffRemovedFg: "#FF5555",
	},
	{
		Name: "nord-dark", DisplayName: "Nord", Appearance: Dark,
		Primary: "#D08770", OnPrimary: "#2E3440", Accent: "#88C0D0", BorderFocus: "#D08770",
		Bg: "#2E3440", Surface: "#3B4252", MutedSurface: "#434C5E", SelectionBg: "#434C5E",
		Text: "#ECEFF4", TextDim: "#D8DEE9", TextMuted: "#7B88A1", Border: "#434C5E",
		Success: "#A3BE8C", Warning: "#EBCB8B", Error: "#BF616A", Info: "#81A1C1",
		SuccessBg: "#3B4A3A", WarningBg: "#48432E", ErrorBg: "#4A3A3D", InfoBg: "#2E3A48", Destructive: "#BF616A",
		PlanMode: "#B48EAD", Subagent: "#B48EAD", RoleUser: "#88C0D0",
		DiffAddedFg: "#A3BE8C", DiffRemovedFg: "#BF616A",
	},
	{
		Name: "jcode-light", DisplayName: "jcode Light", Appearance: Light,
		Primary: "#FF8400", OnPrimary: "#FFFFFF", Accent: "#0891B2", BorderFocus: "#FF8400",
		Bg: "#F2F3F0", Surface: "#FFFFFF", MutedSurface: "#E7E8E5", SelectionBg: "#E7E8E5",
		Text: "#111111", TextDim: "#666666", TextMuted: "#8A8C88", Border: "#CBCCC9",
		Success: "#004D1A", Warning: "#804200", Error: "#8C1C00", Info: "#000066",
		SuccessBg: "#DFE6E1", WarningBg: "#E9E3D8", ErrorBg: "#E5DCDA", InfoBg: "#DFDFE6", Destructive: "#D93C15",
		PlanMode: "#7C3AED", Subagent: "#6D28D9", RoleUser: "#0891B2",
		DiffAddedFg: "#004D1A", DiffRemovedFg: "#8C1C00",
	},
	{
		Name: "github-light", DisplayName: "GitHub Light", Appearance: Light,
		Primary: "#FF8400", OnPrimary: "#FFFFFF", Accent: "#0969DA", BorderFocus: "#FF8400",
		Bg: "#FFFFFF", Surface: "#F6F8FA", MutedSurface: "#EAEEF2", SelectionBg: "#DDF4FF",
		Text: "#1F2328", TextDim: "#656D76", TextMuted: "#8C959F", Border: "#D0D7DE",
		Success: "#1A7F37", Warning: "#9A6700", Error: "#CF222E", Info: "#0969DA",
		SuccessBg: "#DAFBE1", WarningBg: "#FFF8C5", ErrorBg: "#FFEBE9", InfoBg: "#DDF4FF", Destructive: "#CF222E",
		PlanMode: "#8250DF", Subagent: "#8250DF", RoleUser: "#0969DA",
		DiffAddedFg: "#1A7F37", DiffRemovedFg: "#CF222E",
	},
	{
		Name: "solarized-light", DisplayName: "Solarized Light", Appearance: Light,
		Primary: "#CB4B16", OnPrimary: "#FDF6E3", Accent: "#268BD2", BorderFocus: "#CB4B16",
		Bg: "#FDF6E3", Surface: "#EEE8D5", MutedSurface: "#E3DCC4", SelectionBg: "#E3DCC4",
		Text: "#073642", TextDim: "#586E75", TextMuted: "#93A1A1", Border: "#D6CFB7",
		Success: "#5A7000", Warning: "#A57A00", Error: "#DC322F", Info: "#268BD2",
		SuccessBg: "#E3E9C8", WarningBg: "#EEE3C0", ErrorBg: "#F2DAD3", InfoBg: "#D9E6EF", Destructive: "#DC322F",
		PlanMode: "#6C71C4", Subagent: "#6C71C4", RoleUser: "#268BD2",
		DiffAddedFg: "#5A7000", DiffRemovedFg: "#DC322F",
	},
}

var byName = func() map[string]Theme {
	m := make(map[string]Theme, len(builtins))
	for _, t := range builtins {
		m[t.Name] = t
	}
	return m
}()

// All returns the built-in themes in display order (darks first, then lights).
func All() []Theme {
	out := make([]Theme, len(builtins))
	copy(out, builtins)
	return out
}

// Get returns the theme with the given name and whether it exists.
func Get(name string) (Theme, bool) {
	t, ok := byName[name]
	return t, ok
}

// Resolve returns the named theme, or the default dark theme if the name is
// empty or unknown. It always returns a valid theme.
func Resolve(name string) Theme {
	if t, ok := byName[name]; ok {
		return t
	}
	return byName[DefaultDark]
}

// Default returns the built-in default theme name for the given appearance.
func Default(a Appearance) string {
	if a == Light {
		return DefaultLight
	}
	return DefaultDark
}
