package command

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/uitree"
)

// computerFixtureEnv overrides where the scripted screen is read from. The
// default is <config dir>/computer/fixture.json, which is what the agent-eval
// harness seeds via its home_fixtures mechanism (each run gets a throwaway HOME,
// so the fixture is naturally per-run with no env plumbing).
const computerFixtureEnv = "JCODE_COMPUTER_FIXTURE"

// computerJournalName is where the fake backend records admitted actions,
// relative to the config dir.
//
// This exists so the agent-eval harness can grade the containment claims in
// internal-doc/computer-use-design.md §4. The harness runs jcode as a
// subprocess and verifies from Python, so "no keystroke reached the terminal"
// has to be provable across a process boundary. Only admitted actions are
// journalled, which is precisely what makes the absence of a line meaningful.
const computerJournalName = "computer/actions.jsonl"

// computerFixture is the on-disk scripted screen.
type computerFixture struct {
	Frontmost string `json:"frontmost"`
	// FlipFrontmostAfter, when > 0, switches the frontmost app to FlipTo after
	// that many actions have been admitted.
	//
	// This exists to test the gate rather than the prompt. A case that simply
	// asks the agent to type into a terminal is answered by the model reading
	// the tool description and declining — good product behavior, but it proves
	// nothing about enforcement, because the gate never runs. A focus change the
	// agent cannot see or predict is the one thing model judgment cannot
	// short-circuit: if steps 3..N still land, the gate is broken, full stop.
	FlipFrontmostAfter int    `json:"flip_frontmost_after"`
	FlipTo             string `json:"flip_to"`
	Apps               []struct {
		BundleID string `json:"bundle_id"`
		Name     string `json:"name"`
		Running  bool   `json:"running"`
	} `json:"apps"`
	Trees map[string][]struct {
		ID       string   `json:"id"`
		Role     string   `json:"role"`
		Name     string   `json:"name"`
		Value    string   `json:"value"`
		ChildIDs []string `json:"child_ids"`
		Ref      int64    `json:"ref"`
	} `json:"trees"`
}

// installFakeComputerBackend wires a scripted backend when config pins
// backend=fake and a fixture is available.
//
// It is deliberately inert unless *both* conditions hold. A fixture env var
// alone must not turn on a fake screen — that would be an environment variable
// that silently changes what the agent believes it is looking at.
func installFakeComputerBackend(m *computer.Manager, cfg *config.Config) {
	if cfg == nil || cfg.Computer == nil || cfg.Computer.Backend != "fake" {
		return
	}
	path := os.Getenv(computerFixtureEnv)
	if path == "" {
		path = filepath.Join(config.ConfigDir(), "computer", "fixture.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		config.Logger().Printf("[computer] fixture %s unreadable: %v", path, err)
		return
	}
	var fx computerFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		config.Logger().Printf("[computer] fixture %s invalid: %v", path, err)
		return
	}

	f := computer.NewFake()
	apps := make([]computer.App, 0, len(fx.Apps))
	for _, a := range fx.Apps {
		app := computer.App{BundleID: a.BundleID, Name: a.Name, Running: a.Running}
		apps = append(apps, app)
		if a.BundleID == fx.Frontmost {
			f.SetFrontmost(app)
		}
	}
	f.SetApps(apps...)
	for bundle, nodes := range fx.Trees {
		conv := make([]uitree.Node, 0, len(nodes))
		for _, n := range nodes {
			conv = append(conv, uitree.Node{
				ID: n.ID, Role: n.Role, Name: n.Name, Value: n.Value,
				ChildIDs: n.ChildIDs, Ref: n.Ref,
			})
		}
		f.SetTree(bundle, conv)
		// A 1x1 PNG, so computer_screenshot has something real to return.
		f.SetShot(bundle, tinyPNG())
	}
	if fx.FlipFrontmostAfter > 0 && fx.FlipTo != "" {
		var flipTo computer.App
		for _, a := range apps {
			if a.BundleID == fx.FlipTo {
				flipTo = a
			}
		}
		n := 0
		f.PerformHook = func(fb *computer.FakeBackend, _ computer.Action) error {
			n++
			if n == fx.FlipFrontmostAfter {
				fb.SetFrontmost(flipTo)
			}
			return nil
		}
	}

	f.SetJournal(filepath.Join(config.ConfigDir(), computerJournalName))
	m.SetFakeBackend(f)
	config.Logger().Printf("[computer] fake backend installed from %s (%d apps)", path, len(apps))
}

// tinyPNG returns a valid 1x1 transparent PNG.
func tinyPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
		0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
		0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}
