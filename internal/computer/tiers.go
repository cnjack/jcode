package computer

import "strings"

// Tier bounds what may be done to an app, independently of whether the user
// allowlisted it. The allowlist answers "may the agent touch this app at all";
// the tier answers "and how far".
//
// This exists because of one fact about jcode: it runs inside a terminal. An
// agent that can type into that terminal can run `rm -rf`, read
// ~/.jcode/config.json (live API keys), or drive a second jcode — routing around
// jcode's entire approval system by going through the GUI instead of the
// `execute` tool. An approval layer the agent can walk around is decorative.
//
// See internal-doc/computer-use-design.md §4.2.
type Tier int

const (
	// TierRead permits observation only: snapshot and screenshot.
	TierRead Tier = iota
	// TierClick additionally permits pointing: click, hover, scroll. Enough to
	// press a Run button or scroll test output; not enough to enter text.
	TierClick
	// TierFull permits everything, including text entry and key combinations.
	TierFull
)

func (t Tier) String() string {
	switch t {
	case TierRead:
		return "read"
	case TierClick:
		return "click"
	case TierFull:
		return "full"
	}
	return "unknown"
}

// ParseTier maps a config string to a Tier. Unknown values return (TierFull,
// false) so callers can reject rather than silently mis-apply a typo: a typo'd
// tier must never quietly become a *weaker* restriction than intended.
func ParseTier(s string) (Tier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "read":
		return TierRead, true
	case "click":
		return TierClick, true
	case "full":
		return TierFull, true
	}
	return TierFull, false
}

// terminalBundles are terminals and IDEs: TierClick.
//
// Typing here is a total bypass of jcode's approval system (see Tier). Clicking
// and scrolling are useful and safe, so we keep them.
var terminalBundles = map[string]bool{
	"com.apple.Terminal":            true,
	"com.googlecode.iterm2":         true,
	"com.microsoft.VSCode":          true,
	"com.microsoft.VSCodeInsiders":  true,
	"com.todesktop.230313mzl4w4u92": true, // Cursor
	"com.exafunction.windsurf":      true,
	"dev.warp.Warp-Stable":          true,
	"co.zeit.hyper":                 true,
	"net.kovidgoyal.kitty":          true,
	"io.alacritty":                  true,
	"com.github.wez.wezterm":        true,
	"dev.zed.Zed":                   true,
	"com.apple.dt.Xcode":            true,
	"com.sublimetext.4":             true,
	"com.panic.Nova":                true,
}

// terminalPrefixes catches families whose bundle ids we cannot enumerate.
var terminalPrefixes = []string{
	"com.jetbrains.", // IntelliJ, GoLand, PyCharm, …
	"com.google.android.studio",
	"org.eclipse.",
}

// browserBundles are browsers: TierRead.
//
// Not because browsers are dangerous, but because jcode already has a better
// tool for them. browser-use can read the DOM, resolve an href and check an
// origin against the site-permission table before navigating. A pixel click
// cannot see where a link goes, and the visible anchor text is attacker-
// controlled. So the tier does not forbid browser work — it routes it to the
// tool that can enforce safety on it.
var browserBundles = map[string]bool{
	"com.google.Chrome":                   true,
	"com.google.Chrome.canary":            true,
	"com.apple.Safari":                    true,
	"com.apple.SafariTechnologyPreview":   true,
	"org.mozilla.firefox":                 true,
	"org.mozilla.firefoxdeveloperedition": true,
	"com.microsoft.edgemac":               true,
	"com.brave.Browser":                   true,
	"company.thebrowser.Browser":          true, // Arc
	"com.operasoftware.Opera":             true,
	"com.vivaldi.Vivaldi":                 true,
	"com.kagi.kagimacOS":                  true, // Orion
}

// DefaultTier resolves the built-in tier for a bundle id.
//
// Unknown apps get TierFull. That is the honest default: deny-by-default on an
// unknown bundle id breaks every third-party app and trains users to reflexively
// override, which is worse than the thing it prevents. The containment for an
// unknown app is the allowlist (it cannot be touched until the user names and
// approves it), not the tier.
func DefaultTier(bundleID string) Tier {
	if bundleID == "" {
		// An unidentifiable frontmost app is the one case where deny-by-default
		// is right: we cannot show the user what they are approving.
		return TierRead
	}
	if browserBundles[bundleID] {
		return TierRead
	}
	if terminalBundles[bundleID] {
		return TierClick
	}
	for _, p := range terminalPrefixes {
		if strings.HasPrefix(bundleID, p) {
			return TierClick
		}
	}
	return TierFull
}

// IsBrowser reports whether the bundle id is a known browser, so callers can
// point the model at browser-use rather than just refusing.
func IsBrowser(bundleID string) bool { return browserBundles[bundleID] }

// requiredTier is the minimum tier an action needs.
//
// The split follows the capability, not the input device: `scroll` is TierClick
// because scrolling test output in an IDE is safe, while `press` is TierFull
// because cmd+N in a terminal opens a shell.
func requiredTier(action string) Tier {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "click", "hover", "scroll":
		return TierClick
	case "dblclick", "rclick", "type", "press", "set_value", "drag", "select_text", "menu":
		return TierFull
	}
	// Unknown actions are gated at the strictest tier rather than waved through.
	return TierFull
}

// Allows reports whether tier t permits action.
func (t Tier) Allows(action string) bool { return t >= requiredTier(action) }
