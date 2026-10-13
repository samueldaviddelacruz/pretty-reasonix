package config_test

import (
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
)

// resolve pairs one user file with one project file and returns what was refused
// alongside the configuration that resulted, which is what every case below is
// about: not only that a value was refused, but that the user's value stands.
func resolve(t *testing.T, userBody, projectBody string) (config.IgnoredProjectSettings, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	user := writeTemp(t, dir, "user.toml", userBody)
	project := writeTemp(t, dir, "project.toml", projectBody)
	result, err := config.Load(project, user, config.Flags{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return result.Ignored, result.Config
}

// widenAttempt is one setting a project may not widen.
//
// `key` is what the notice has to name, and it is what ties these rows to
// config.NarrowOnlyKeys — see TestEveryRuleHasAWidenAttempt.
type widenAttempt struct {
	name    string
	user    string
	project string
	key     string
	assert  func(t *testing.T, cfg *config.Config)
}

// widenAttempts is the table the acceptance criterion asks for, as a function so
// that the coverage test can read the same rows the behaviour test runs.
func widenAttempts() []widenAttempt {
	return []widenAttempt{
		{
			name:    "an allow rule",
			user:    "[permissions]\nmode = \"ask\"\n",
			project: "[permissions]\nallow = [\"Bash(git push:*)\"]\n",
			key:     "permissions.allow",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Permissions.Allow) != 0 {
					t.Errorf("allow = %v, but the user granted nothing and the project granted itself a rule", cfg.Permissions.Allow)
				}
			},
		},
		{
			name:    "the fallback toward allowing",
			user:    "[permissions]\nmode = \"ask\"\n",
			project: "[permissions]\nmode = \"allow\"\n",
			key:     "permissions.mode",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Permissions.Mode != "ask" {
					t.Errorf("mode = %q, want the user's ask", cfg.Permissions.Mode)
				}
			},
		},
		{
			name:    "dropping a prompt",
			user:    "[permissions]\nask = [\"Bash(rm:*)\"]\n",
			project: "[permissions]\nask = []\n",
			key:     "permissions.ask",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Permissions.Ask) != 1 {
					t.Errorf("ask = %v, want the user's prompt kept", cfg.Permissions.Ask)
				}
			},
		},
		{
			name:    "network access",
			user:    "[sandbox]\nnetwork = false\n",
			project: "[sandbox]\nnetwork = true\n",
			key:     "sandbox.network",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Sandbox.Network {
					t.Error("network = true, want the user's false")
				}
			},
		},
		{
			name:    "the shell jail",
			user:    "[sandbox]\nbash = \"enforce\"\n",
			project: "[sandbox]\nbash = \"off\"\n",
			key:     "sandbox.bash",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Sandbox.Bash != "enforce" {
					t.Errorf("bash = %q, want the user's enforce", cfg.Sandbox.Bash)
				}
			},
		},
		{
			name:    "a writable root",
			user:    "[sandbox]\nallow_write = [\"/tmp\"]\n",
			project: "[sandbox]\nallow_write = [\"/tmp\", \"/\"]\n",
			key:     "sandbox.allow_write",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Sandbox.AllowWrite) != 1 || cfg.Sandbox.AllowWrite[0] != "/tmp" {
					t.Errorf("allow_write = %v, want the user's single root", cfg.Sandbox.AllowWrite)
				}
			},
		},
		{
			name:    "an unprotected path",
			user:    "[sandbox]\nforbid_read = [\"/home/tester/.ssh\"]\n",
			project: "[sandbox]\nforbid_read = []\n",
			key:     "sandbox.forbid_read",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Sandbox.ForbidRead) != 1 {
					t.Errorf("forbid_read = %v, want the user's protected path kept", cfg.Sandbox.ForbidRead)
				}
			},
		},
		{
			name:    "the tool surface",
			user:    "[tools]\nenabled = [\"read_file\"]\n",
			project: "[tools]\nenabled = []\n",
			key:     "tools.enabled",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Tools.Enabled) != 1 {
					t.Errorf("enabled = %v, want the user's restriction kept", cfg.Tools.Enabled)
				}
			},
		},
	}
}

// TestAProjectCannotWiden is the first half of the criterion: every attempt is
// refused, the notice names the key, and the user's value stands.
func TestAProjectCannotWiden(t *testing.T) {
	t.Parallel()
	for _, tc := range widenAttempts() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ignored, cfg := resolve(t, tc.user, tc.project)

			if len(ignored) != 1 {
				t.Fatalf("want exactly one notice, got %d: %v", len(ignored), ignored)
			}
			if ignored[0].Key != tc.key {
				t.Errorf("the notice names %q, want %q", ignored[0].Key, tc.key)
			}
			if ignored[0].Source == "" {
				t.Error("the notice does not say which file asked")
			}
			if ignored[0].Note == "" {
				t.Error("the notice does not say why")
			}
			tc.assert(t, cfg)
		})
	}
}

// TestAProjectMayNarrow is the other half, and it is not decoration: without it the
// test above would pass on an implementation that refused every project setting,
// which is the opposite of the feature.
func TestAProjectMayNarrow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    string
		project string
		assert  func(t *testing.T, cfg *config.Config)
	}{
		{
			name:    "adding a deny rule",
			user:    "[permissions]\nmode = \"ask\"\n",
			project: "[permissions]\ndeny = [\"Bash(rm -rf:*)\"]\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Permissions.Deny) != 1 {
					t.Errorf("deny = %v, want the project's rule kept", cfg.Permissions.Deny)
				}
			},
		},
		{
			name:    "protecting another path",
			user:    "[sandbox]\nforbid_read = [\"/home/tester/.ssh\"]\n",
			project: "[sandbox]\nforbid_read = [\"/home/tester/.ssh\", \"/etc\"]\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Sandbox.ForbidRead) != 2 {
					t.Errorf("forbid_read = %v, want both paths; adding a protected path is a narrowing", cfg.Sandbox.ForbidRead)
				}
			},
		},
		{
			name:    "turning network off",
			user:    "[sandbox]\nnetwork = true\n",
			project: "[sandbox]\nnetwork = false\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Sandbox.Network {
					t.Error("network = true, want the project's false kept")
				}
			},
		},
		{
			name:    "moving the fallback toward denying",
			user:    "[permissions]\nmode = \"allow\"\n",
			project: "[permissions]\nmode = \"deny\"\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.Permissions.Mode != "deny" {
					t.Errorf("mode = %q, want the project's deny", cfg.Permissions.Mode)
				}
			},
		},
		{
			name:    "removing a writable root",
			user:    "[sandbox]\nallow_write = [\"/tmp\", \"/var/tmp\"]\n",
			project: "[sandbox]\nallow_write = [\"/tmp\"]\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if len(cfg.Sandbox.AllowWrite) != 1 {
					t.Errorf("allow_write = %v, want the project's narrower list", cfg.Sandbox.AllowWrite)
				}
			},
		},
		{
			name:    "narrowing the tool surface",
			user:    "[tools]\nenabled = []\n",
			project: "[tools]\nenabled = [\"read_file\"]\n",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				// Empty means every built-in, so listing one is a restriction —
				// the case that made the rule get its comparison the other way
				// round.
				if len(cfg.Tools.Enabled) != 1 || cfg.Tools.Enabled[0] != "read_file" {
					t.Errorf("enabled = %v, want the project's single tool kept", cfg.Tools.Enabled)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ignored, cfg := resolve(t, tc.user, tc.project)
			if len(ignored) != 0 {
				t.Fatalf("a narrowing was refused: %v", ignored)
			}
			tc.assert(t, cfg)
		})
	}
}

// TestEveryRuleHasAWidenAttempt keeps the rules and the table in step. A rule added
// to NarrowOnlyKeys with no row in widenAttempts would be a refusal that nothing
// asserts — and the check is on the key the row claims, so a row that pointed at the
// wrong setting would fail too.
func TestEveryRuleHasAWidenAttempt(t *testing.T) {
	t.Parallel()
	covered := map[string]bool{}
	for _, tc := range widenAttempts() {
		covered[tc.key] = true
	}
	keys := config.NarrowOnlyKeys()
	if len(keys) == 0 {
		t.Fatal("NarrowOnlyKeys is empty, so a project may widen anything")
	}
	for _, key := range keys {
		if !covered[key] {
			t.Errorf("%s may not be widened by a project, and no attempt here tries to", key)
		}
	}
	if len(covered) != len(keys) {
		t.Errorf("the table covers %d keys and %d rules are declared; one of them names a rule that does not exist", len(covered), len(keys))
	}
}

// TestIgnoredProjectSettingsListsTheKeys checks the summary a caller shows, since
// listing the keys is what the criterion asks for.
func TestIgnoredProjectSettingsListsTheKeys(t *testing.T) {
	t.Parallel()
	ignored, _ := resolve(t,
		"[permissions]\nmode = \"ask\"\n[sandbox]\nnetwork = false\n",
		"[permissions]\nallow = [\"Bash(x:*)\"]\n[sandbox]\nnetwork = true\n")

	if len(ignored) != 2 {
		t.Fatalf("want two notices, got %d: %v", len(ignored), ignored)
	}
	summary := ignored.String()
	for _, key := range []string{"permissions.allow", "sandbox.network"} {
		if !strings.Contains(summary, key) {
			t.Errorf("the summary %q does not name %s", summary, key)
		}
	}

	empty := config.IgnoredProjectSettings(nil).String()
	if !strings.Contains(empty, "nothing") {
		t.Errorf("an empty list summarises as %q, want it to say the project asked for nothing", empty)
	}
}
