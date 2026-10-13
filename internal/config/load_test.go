package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"pretty-reasonix/internal/config"
)

// layerCase is one section's resolution order: the same setting named by the
// user file and by the project file, with the winner asserted.
//
// Each row asserts two things, and the second is the one that matters. The first
// is that the higher layer wins; the second is that a key only the *lower* layer
// names survives — which is what merging at the decode step buys and what
// overlaying whole structs would lose.
type layerCase struct {
	section string
	user    string
	project string
	flags   config.Flags
	assert  func(t *testing.T, cfg *config.Config)
}

func TestResolutionOrderPerSection(t *testing.T) {
	t.Parallel()
	cases := []layerCase{
		{
			section: "ui",
			user:    "[ui]\ntheme = \"dark\"\nstatusline = \"full\"\n",
			project: "[ui]\ntheme = \"light\"\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.UI.Theme != "light" {
					t.Errorf("ui.theme = %q, want the project's light", cfg.UI.Theme)
				}
				if cfg.UI.StatusLine != "full" {
					t.Errorf("ui.statusline = %q, want the user's full: the project never mentioned it", cfg.UI.StatusLine)
				}
			},
		},
		{
			section: "agent",
			user:    "[agent]\ncompact_ratio = 0.7\noutput_style = \"terse\"\n",
			project: "[agent]\ncompact_ratio = 0.8\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Agent.CompactRatio != 0.8 {
					t.Errorf("agent.compact_ratio = %v, want the project's 0.8", cfg.Agent.CompactRatio)
				}
				if cfg.Agent.OutputStyle != "terse" {
					t.Errorf("agent.output_style = %q, want the user's terse", cfg.Agent.OutputStyle)
				}
			},
		},
		{
			section: "tools",
			user:    "[tools]\nbash_timeout_seconds = 30\nprotect_changed_files = false\n",
			project: "[tools]\nbash_timeout_seconds = 10\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Tools.BashTimeoutSeconds != 10 {
					t.Errorf("tools.bash_timeout_seconds = %d, want the project's 10", cfg.Tools.BashTimeoutSeconds)
				}
				if cfg.Tools.ProtectChangedFiles {
					t.Error("tools.protect_changed_files = true, want the user's false: the project never mentioned it")
				}
			},
		},
		{
			section: "permissions",
			user:    "[permissions]\nmode = \"ask\"\nallow = [\"Bash(git status:*)\"]\n",
			project: "[permissions]\nmode = \"deny\"\n",
			flags:   config.Flags{Allow: []string{"Bash(go test:*)"}},
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Permissions.Mode != "deny" {
					t.Errorf("permissions.mode = %q, want the project's deny", cfg.Permissions.Mode)
				}
				// The flag appends to the resolved list rather than replacing it,
				// so a command line can grant but never revoke.
				want := []string{"Bash(git status:*)", "Bash(go test:*)"}
				if len(cfg.Permissions.Allow) != len(want) {
					t.Fatalf("permissions.allow = %v, want the user's rule plus the flag's", cfg.Permissions.Allow)
				}
				for i, rule := range want {
					if cfg.Permissions.Allow[i] != rule {
						t.Errorf("permissions.allow[%d] = %q, want %q", i, cfg.Permissions.Allow[i], rule)
					}
				}
			},
		},
		{
			section: "sandbox",
			user:    "[sandbox]\nallow_write = [\"/tmp\"]\n",
			project: "[sandbox]\nnetwork = false\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				// A project turning network off is narrowing, and is obeyed even
				// though the default is true.
				if cfg.Sandbox.Network {
					t.Error("sandbox.network = true, want the project's false overriding the default")
				}
				if len(cfg.Sandbox.AllowWrite) != 1 || cfg.Sandbox.AllowWrite[0] != "/tmp" {
					t.Errorf("sandbox.allow_write = %v, want the user's /tmp", cfg.Sandbox.AllowWrite)
				}
			},
		},
		{
			section: "skills",
			user:    "[skills]\ndisabled_skills = [\"review\"]\n",
			project: "[skills]\ndisabled_skills = [\"test\"]\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				// A list is replaced rather than concatenated: two layers naming
				// the same list is a disagreement, and the higher layer is the
				// one that answers it.
				if len(cfg.Skills.DisabledSkills) != 1 || cfg.Skills.DisabledSkills[0] != "test" {
					t.Errorf("skills.disabled_skills = %v, want exactly the project's list", cfg.Skills.DisabledSkills)
				}
			},
		},
		{
			section: "environment",
			user:    "[environment]\noffline = true\n",
			project: "[environment]\nenabled = false\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Environment.Enabled {
					t.Error("environment.enabled = true, want the project's false")
				}
				if !cfg.Environment.Offline {
					t.Error("environment.offline = false, want the user's true")
				}
			},
		},
		{
			section: "providers",
			user:    "[[providers]]\nname = \"user-vendor\"\nkind = \"openai\"\n",
			project: "[[providers]]\nname = \"project-vendor\"\nkind = \"anthropic\"\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				// Asserted loosely on purpose. Whether a later [[providers]] array
				// *adds to* or *replaces* the earlier one is a real question with
				// a safe answer and an unsafe one, and it belongs to the ticket
				// that first merges providers (E3-T10). Here the only claim that
				// holds either way is that the project's entry is present.
				names := make([]string, 0, len(cfg.Providers))
				for _, provider := range cfg.Providers {
					names = append(names, provider.Name)
				}
				found := false
				for _, name := range names {
					if name == "project-vendor" && len(names) >= 1 {
						found = true
					}
				}
				if !found {
					t.Errorf("the project's provider is missing; providers = %v", names)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			user := writeTemp(t, dir, "user.toml", tc.user)
			project := writeTemp(t, dir, "project.toml", tc.project)

			cfg, warnings, err := config.Load(project, user, tc.flags)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
			tc.assert(t, cfg)
		})
	}
}

// TestFlagsBeatBothFiles checks the top of the order, which the per-section table
// above only covers for the sections a flag can reach.
func TestFlagsBeatBothFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	user := writeTemp(t, dir, "user.toml", "default_model = \"from-user\"\n[permissions]\nmode = \"ask\"\n")
	project := writeTemp(t, dir, "project.toml", "default_model = \"from-project\"\n[permissions]\nmode = \"allow\"\n")

	cfg, _, err := config.Load(project, user, config.Flags{
		Model:          new("from-flag"),
		PermissionMode: new("deny"),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultModel != "from-flag" {
		t.Errorf("default_model = %q, want the flag's value", cfg.DefaultModel)
	}
	if cfg.Permissions.Mode != "deny" {
		t.Errorf("permissions.mode = %q, want the flag's value", cfg.Permissions.Mode)
	}
}

// TestMissingFilesAreOrdinary checks that the two files a run usually does not
// have are skipped rather than fatal, and that the result is then exactly the
// defaults.
func TestMissingFilesAreOrdinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.toml")

	cfg, warnings, err := config.Load(absent, absent, config.Flags{Model: new("from-flag")})
	if err != nil {
		t.Fatalf("Load with no files at all: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg.DefaultModel != "from-flag" {
		t.Errorf("default_model = %q, want the flag still applied over the defaults", cfg.DefaultModel)
	}
	if cfg.Agent.CompactRatio != config.Default().Agent.CompactRatio {
		t.Error("a setting no source named did not come from the defaults")
	}
}

// TestUnknownKeysAreAttributedToTheirFile checks the warning carries the layer it
// came from, because `prx doctor` has to say which file to edit.
func TestUnknownKeysAreAttributedToTheirFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	user := writeTemp(t, dir, "user.toml", "[ui]\ntheme = \"dark\"\nmystery = 1\n")
	project := writeTemp(t, dir, "project.toml", "[agent]\nalso_mystery = 2\n")

	_, warnings, err := config.Load(project, user, config.Flags{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	bySource := map[string]string{}
	for _, w := range warnings {
		bySource[w.Source] = w.Key
	}
	if bySource[user] != "ui.mystery" {
		t.Errorf("user file warnings = %v, want ui.mystery attributed to %s", bySource, user)
	}
	if bySource[project] != "agent.also_mystery" {
		t.Errorf("project file warnings = %v, want agent.also_mystery attributed to %s", bySource, project)
	}
}

// writeTemp writes a file a test will load, and returns its path.
func writeTemp(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}
