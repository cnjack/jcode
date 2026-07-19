package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// ExtensionID is the unpacked/dev extension id, derived from the committed
// public key ("key" field) in extension/manifest.json — stable across loads.
const ExtensionID = "ekcnniaefmnhnemnpphikhgfoofnojnd"

// AllowedExtensionIDs is every id the Browser Bridge extension can have: the
// unpacked dev build (above) plus the published store builds, which the stores
// re-sign under their own ids. All of these must appear in the native-host
// manifest's allowed_origins, or a store-installed extension can't open the
// native host (the browser enforces the origin before launching it).
var AllowedExtensionIDs = []string{
	ExtensionID,                        // unpacked / dev (manifest "key")
	"olkapiiikpfhaccmjphakolinkcggcbd", // Chrome Web Store
	// Microsoft Edge Add-ons: add the runtime extension id here once known. It is
	// NOT the Partner Center product id (0RDCKGMRP90R) — that's the listing id.
	// Find the runtime id at edge://extensions after installing, or in Partner
	// Center → Extension overview.
}

// FindChrome returns the path to a Chromium-based browser executable, or ""
// when none is found. Explicit configPath (config.browser.chrome_path) wins.
func FindChrome(configPath string) string {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
	default: // linux & friends
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, p)
			}
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// ChromeVersion returns the version string reported by the executable.
func ChromeVersion(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// chromeProfileDirs returns candidate Chrome user-data dirs for extension
// detection (the user's real Chrome, not our managed profile).
func chromeProfileDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library/Application Support/Google/Chrome")}
	case "windows":
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return []string{filepath.Join(lad, `Google\Chrome\User Data`)}
		}
		return nil
	default:
		return []string{filepath.Join(home, ".config/google-chrome"), filepath.Join(home, ".config/chromium")}
	}
}

// ExtensionInstallState reports whether a jcode extension is present in the
// user's Chrome profiles by scanning Preferences JSON — the same technique as
// Codex's check-extension-installed.js.
type ExtensionInstallState struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Profile   string `json:"profile,omitempty"`
	Path      string `json:"path,omitempty"` // unpacked path when known
}

// CheckExtensionInstalled scans profileRoots (or the default Chrome dirs when
// nil) for an extension whose unpacked path points at extDir, or whose id
// equals ExtensionID when set.
func CheckExtensionInstalled(profileRoots []string, extDir string) ExtensionInstallState {
	if profileRoots == nil {
		profileRoots = chromeProfileDirs()
	}
	extDir = filepath.Clean(extDir)
	for _, root := range profileRoots {
		profiles, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, p := range profiles {
			if !p.IsDir() {
				continue
			}
			name := p.Name()
			if name != "Default" && !strings.HasPrefix(name, "Profile ") {
				continue
			}
			for _, prefFile := range []string{"Preferences", "Secure Preferences"} {
				st := scanPreferences(filepath.Join(root, name, prefFile), extDir)
				if st.Installed {
					st.Profile = name
					return st
				}
			}
		}
	}
	return ExtensionInstallState{}
}

func scanPreferences(prefPath, extDir string) ExtensionInstallState {
	data, err := os.ReadFile(prefPath)
	if err != nil {
		return ExtensionInstallState{}
	}
	var prefs struct {
		Extensions struct {
			Settings map[string]struct {
				Path           string `json:"path"`
				State          int    `json:"state"`
				DisableReasons any    `json:"disable_reasons"`
			} `json:"settings"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return ExtensionInstallState{}
	}
	for id, s := range prefs.Extensions.Settings {
		matched := (ExtensionID != "" && id == ExtensionID) ||
			(s.Path != "" && filepath.Clean(s.Path) == extDir)
		if !matched {
			continue
		}
		return ExtensionInstallState{Installed: true, Enabled: s.State == 1, Path: s.Path}
	}
	return ExtensionInstallState{}
}

// ---------------------------------------------------------------------------
// Launch — start a managed Chrome with an isolated profile and connect.
// ---------------------------------------------------------------------------

// LaunchOptions controls the managed Chrome launch.
type LaunchOptions struct {
	ChromePath string // empty → FindChrome
	Headless   bool
	ProfileDir string // empty → ~/.jcode/browser/profile
	Viewport   string // "1280x720"
}

var devtoolsRe = regexp.MustCompile(`DevTools listening on (ws://[^\s]+)`)

// managedChromeEnv returns the child environment for managed Chrome. On macOS,
// Chromium consults HOME for framework/runtime resources in addition to the
// explicitly isolated --user-data-dir. jcode evaluation and server processes
// may intentionally run with an isolated HOME, so use the OS account home for
// Chrome only. Other platforms (and invalid account homes) retain their HOME.
// The Web bearer token is never needed by Chrome and must not cross this process
// boundary.
func managedChromeEnv(goos string, environ []string, systemHome string) []string {
	useSystemHome := goos == "darwin" && systemHome == strings.TrimSpace(systemHome) && filepath.IsAbs(systemHome)
	result := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key == "JCODE_WEB_TOKEN" {
			continue
		}
		if useSystemHome && ok && key == "HOME" {
			continue
		}
		result = append(result, entry)
	}
	if useSystemHome {
		result = append(result, "HOME="+systemHome)
	}
	return result
}

func existingHomeDir(path string) string {
	if path == "" || path != strings.TrimSpace(path) || !filepath.IsAbs(path) {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

func managedChromeProcessEnv() []string {
	systemHome := ""
	if current, err := user.Current(); err == nil {
		// Validate the account database result before allowing it to replace HOME.
		// managedChromeEnv stays a pure transformer and defensively rechecks its
		// shape; filesystem existence belongs at this OS-facing boundary.
		systemHome = existingHomeDir(current.HomeDir)
	}
	return managedChromeEnv(runtime.GOOS, os.Environ(), systemHome)
}

// Launch starts Chrome with --remote-debugging-port=0, waits for the DevTools
// websocket announcement on stderr, and returns a connected managed backend.
func Launch(ctx context.Context, opts LaunchOptions) (Backend, error) {
	chrome := FindChrome(opts.ChromePath)
	if chrome == "" {
		return nil, fmt.Errorf("no Chromium-based browser found; set browser.chrome_path in config")
	}
	profile := opts.ProfileDir
	if profile == "" {
		profile = filepath.Join(config.ConfigDir(), "browser", "profile")
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}

	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-features=Translate",
		"--new-window",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	if opts.Viewport != "" {
		args = append(args, "--window-size="+strings.Replace(opts.Viewport, "x", ",", 1))
	}
	args = append(args, "about:blank")

	cmd := exec.Command(chrome, args...)
	cmd.Env = managedChromeProcessEnv()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	wsCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if m := devtoolsRe.FindStringSubmatch(scanner.Text()); m != nil {
				select {
				case wsCh <- m[1]:
				default:
				}
				// Keep draining so Chrome never blocks on a full stderr pipe.
			}
		}
	}()

	launchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case wsURL := <-wsCh:
		stop := func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		backend, err := connectManaged(launchCtx, wsURL, stop)
		if err != nil {
			stop()
			return nil, err
		}
		config.Logger().Printf("[browser] managed chrome started pid=%d ws=%s", cmd.Process.Pid, wsURL)
		return backend, nil
	case <-launchCtx.Done():
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("chrome did not announce DevTools endpoint within 30s")
	}
}
