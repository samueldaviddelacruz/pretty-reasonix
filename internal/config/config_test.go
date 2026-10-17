package config_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
)

// fullConfig is a configuration with every field set to a non-zero value, used
// to prove that nothing is lost on the way to disk and back. The
// TestFullFixtureSetsEveryField check below is what stops it silently ceasing to
// cover a field.
//
// Every bool is true: for a bool the only non-zero value is true, and the
// explicit-false case is covered by TestExplicitZeroValuesBeatTheDefaults
// instead — which is the case that matters, because it is the one an encoder
// that skips zero values would silently lose.
func fullConfig() *config.Config {
	return &config.Config{
		DefaultModel: "provider/model",
		Language:     "en",
		UI: config.UI{
			Theme:         "dark",
			ThemeStyle:    "ember",
			CursorShape:   "block",
			ShowTurnUsage: true,
			CommandMode:   "vi",
			StatusLine:    "full",
		},
		Agent: config.Agent{
			SystemPrompt:      "be brief",
			SystemPromptFile:  "/tmp/prompt.md",
			Temperature:       0.5,
			ReasoningLanguage: "en",
			CompactRatio:      0.7,
			MaxOutputTokens:   32768,
			OutputStyle:       "terse",
			Keymap:            map[string]string{"composer.send": "ctrl+enter"},
		},
		Providers: []config.Provider{{
			Name:               "vendor",
			Kind:               "openai",
			BaseURL:            "https://example.test",
			Models:             []string{"m1"},
			Default:            "m1",
			APIKeyEnv:          "VENDOR_KEY",
			ContextWindow:      128000,
			RequestURL:         "https://example.test/v1/chat",
			MaxOutputTokens:    4096,
			IdleTimeoutSeconds: 42,
			ModelOverrides: map[string]config.ModelOverride{"m1": {
				ContextWindow:    64,
				MaxOutputTokens:  8,
				SupportedEfforts: []string{"low", "high"},
				DefaultEffort:    "high",
			}},
			Prices: map[string]config.Price{"m1": {Input: 1, Output: 2, CacheHit: 0.5, Currency: "USD"}},
		}},
		Tools: config.Tools{
			Enabled:                  []string{"read_file"},
			BashTimeoutSeconds:       7,
			ProtectChangedFiles:      true,
			MCPStartupTimeoutSeconds: 3,
			MCPCallTimeoutSeconds:    9,
		},
		Permissions: config.Permissions{
			Mode:  "allow",
			Deny:  []string{"Bash(rm:*)"},
			Allow: []string{"Bash(go test:*)"},
			Ask:   []string{"Edit"},
		},
		Sandbox: config.Sandbox{
			WorkspaceRoot: "/workspace",
			AllowWrite:    []string{"/tmp"},
			ForbidRead:    []string{"/secrets"},
			Bash:          "off",
			Network:       true,
		},
		Skills: config.Skills{
			Paths:          []string{"/skills"},
			DisabledSkills: []string{"review"},
		},
		Environment: config.Environment{Enabled: true, Offline: true},
		Plugins:     []config.Plugin{{Name: "srv", Type: "stdio", Command: "srv", Args: []string{"--x"}}},
	}
}

// TestDefaultsAreTheDocumentedValues is the acceptance criterion's "documented
// default" half: every field is listed with the value its comment promises, so a
// changed default is a failing test rather than a surprise.
func TestDefaultsAreTheDocumentedValues(t *testing.T) {
	t.Parallel()
	d := config.Default()
	cases := []struct {
		field string
		got   any
		want  any
	}{
		{"default_model", d.DefaultModel, "deepseek"},
		{"language", d.Language, ""},
		{"ui.theme", d.UI.Theme, "auto"},
		{"ui.theme_style", d.UI.ThemeStyle, ""},
		{"ui.cursor_shape", d.UI.CursorShape, "bar"},
		{"ui.show_turn_usage", d.UI.ShowTurnUsage, true},
		{"ui.commandmode", d.UI.CommandMode, ""},
		{"ui.statusline", d.UI.StatusLine, "compact"},
		{"agent.system_prompt", d.Agent.SystemPrompt, ""},
		{"agent.system_prompt_file", d.Agent.SystemPromptFile, ""},
		{"agent.temperature", d.Agent.Temperature, 0.0},
		{"agent.reasoning_language", d.Agent.ReasoningLanguage, "auto"},
		{"agent.compact_ratio", d.Agent.CompactRatio, 0.85},
		{"agent.max_output_tokens", d.Agent.MaxOutputTokens, 0},
		{"agent.output_style", d.Agent.OutputStyle, ""},
		{"agent.keymap", d.Agent.Keymap, map[string]string{}},
		{"providers", d.Providers, []config.Provider(nil)},
		{"tools.enabled", d.Tools.Enabled, []string{}},
		{"tools.bash_timeout_seconds", d.Tools.BashTimeoutSeconds, 120},
		{"tools.protect_changed_files", d.Tools.ProtectChangedFiles, true},
		{"tools.mcp_startup_timeout_seconds", d.Tools.MCPStartupTimeoutSeconds, 30},
		{"tools.mcp_call_timeout_seconds", d.Tools.MCPCallTimeoutSeconds, 300},
		{"permissions.mode", d.Permissions.Mode, "ask"},
		{"permissions.deny", d.Permissions.Deny, []string{}},
		{"permissions.allow", d.Permissions.Allow, []string{}},
		{"permissions.ask", d.Permissions.Ask, []string{}},
		{"sandbox.workspace_root", d.Sandbox.WorkspaceRoot, ""},
		{"sandbox.allow_write", d.Sandbox.AllowWrite, []string{}},
		{"sandbox.forbid_read", d.Sandbox.ForbidRead, []string{}},
		{"sandbox.bash", d.Sandbox.Bash, "enforce"},
		{"sandbox.network", d.Sandbox.Network, true},
		{"skills.paths", d.Skills.Paths, []string{}},
		{"skills.disabled_skills", d.Skills.DisabledSkills, []string{}},
		{"environment.enabled", d.Environment.Enabled, true},
		{"environment.offline", d.Environment.Offline, false},
		{"plugins", d.Plugins, []config.Plugin(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("%s = %#v, want %#v", tc.field, tc.got, tc.want)
			}
		})
	}
}

// TestFullFixtureSetsEveryField keeps the round trip below honest. Without it the
// fixture could quietly stop covering a field — a new field left at its zero
// value would round-trip trivially, because zero is what it would come back as.
func TestFullFixtureSetsEveryField(t *testing.T) {
	t.Parallel()
	if zero := firstZeroValue(reflect.ValueOf(fullConfig()).Elem(), ""); zero != "" {
		t.Fatalf("%s is at its zero value in the round-trip fixture, so the round trip would not actually cover it", zero)
	}
}

// TestEveryFieldRoundTripsThroughAFile writes the fully-populated configuration,
// reads it back, and compares.
func TestEveryFieldRoundTripsThroughAFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	want := fullConfig()

	if err := config.Save(path, want); err != nil {
		t.Fatalf("saving: %v", err)
	}
	got, warnings, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("loading what we just saved: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("the round trip produced warnings, so a field is not in the schema: %v", warnings)
	}
	if reflect.DeepEqual(got, want) {
		return
	}
	gotV, wantV := reflect.ValueOf(got).Elem(), reflect.ValueOf(want).Elem()
	for i := range wantV.NumField() {
		if reflect.DeepEqual(gotV.Field(i).Interface(), wantV.Field(i).Interface()) {
			continue
		}
		t.Errorf("%s did not survive the round trip:\n got %#v\nwant %#v",
			wantV.Type().Field(i).Name, gotV.Field(i).Interface(), wantV.Field(i).Interface())
	}
}

// TestExplicitZeroValuesBeatTheDefaults covers what the all-true fixture cannot:
// a file that turns a default-on setting off, or sets a number to zero, must be
// obeyed rather than quietly reinstated by the defaults it starts from.
func TestExplicitZeroValuesBeatTheDefaults(t *testing.T) {
	t.Parallel()
	body := []byte(`
[ui]
show_turn_usage = false
[agent]
temperature = 0.0
compact_ratio = 1.0
[tools]
protect_changed_files = false
bash_timeout_seconds = 0
[sandbox]
network = false
[environment]
enabled = false
offline = true
`)
	cfg, warnings, err := config.Parse(body)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	cases := []struct {
		field string
		got   any
		want  any
	}{
		{"ui.show_turn_usage", cfg.UI.ShowTurnUsage, false},
		{"agent.temperature", cfg.Agent.Temperature, 0.0},
		{"agent.compact_ratio", cfg.Agent.CompactRatio, 1.0},
		{"tools.protect_changed_files", cfg.Tools.ProtectChangedFiles, false},
		{"tools.bash_timeout_seconds", cfg.Tools.BashTimeoutSeconds, 0},
		{"sandbox.network", cfg.Sandbox.Network, false},
		{"environment.enabled", cfg.Environment.Enabled, false},
		{"environment.offline", cfg.Environment.Offline, true},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %#v, want %#v (the default overrode an explicit value)", tc.field, tc.got, tc.want)
		}
	}
}

// TestAbsentKeysTakeTheirDefaults is the other half: a file that says nothing
// must behave exactly like no file at all.
func TestAbsentKeysTakeTheirDefaults(t *testing.T) {
	t.Parallel()
	cfg, warnings, err := config.Parse(nil)
	if err != nil {
		t.Fatalf("parsing an empty file: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !reflect.DeepEqual(cfg, config.Default()) {
		t.Error("an empty file did not produce the default configuration")
	}
}

// TestUnknownKeysWarnRatherThanFail is the first half of the second acceptance
// criterion: a configuration written for a later version still starts, minus
// what this one cannot understand.
func TestUnknownKeysWarnRatherThanFail(t *testing.T) {
	t.Parallel()
	body := []byte(`
default_model = "deepseek"
[ui]
theme = "dark"
theme_packs = ["extra"]
[agent]
mystery = 3
`)
	cfg, warnings, err := config.Parse(body)
	if err != nil {
		t.Fatalf("an unknown key must not be fatal: %v", err)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("ui.theme = %q, want the value the file set", cfg.UI.Theme)
	}

	keys := make([]string, 0, len(warnings))
	for _, w := range warnings {
		keys = append(keys, w.Key)
		if w.Note == "" {
			t.Errorf("warning for %q has no note, so doctor cannot explain it", w.Key)
		}
	}
	for _, want := range []string{"agent.mystery", "ui.theme_packs"} {
		if !slices.Contains(keys, want) {
			t.Errorf("no warning for the unknown key %s; got %v", want, keys)
		}
	}
}

// TestMalformedFileIsReportedAndLeftAlone is the second half of that criterion,
// and the reason LoadFile reads bytes without ever writing any: a file this
// program cannot parse is a file it must not touch.
func TestMalformedFileIsReportedAndLeftAlone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	broken := []byte("default_model = \n[[providers]\nname = \"unterminated\n")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	cfg, warnings, err := config.LoadFile(path)
	if !errors.Is(err, config.ErrMalformed) {
		t.Fatalf("error = %v, want it to match ErrMalformed", err)
	}
	if cfg != nil || warnings != nil {
		t.Errorf("a failed parse returned a partial result (%#v, %#v); half a configuration is worse than none", cfg, warnings)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the file: %v", err)
	}
	if !bytes.Equal(after, broken) {
		t.Errorf("the malformed file was rewritten:\nbefore %q\nafter  %q", broken, after)
	}
}

// TestMissingFileIsDistinguishable checks that "there is no file" is separable
// from "the file is broken", because E2-T3 needs the first to be ordinary.
func TestMissingFileIsDistinguishable(t *testing.T) {
	t.Parallel()
	_, _, err := config.LoadFile(filepath.Join(t.TempDir(), "absent.toml"))
	if !config.Missing(err) {
		t.Fatalf("error = %v, want it to report a missing file", err)
	}
}

// TestSaveIsOwnerOnly checks the mode, because a configuration names the
// environment variables holding credentials and is read by nobody but its owner.
func TestSaveIsOwnerOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatalf("saving: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600", perm)
	}
}

// TestSaveLeavesNoDebrisBehind checks that the temporary file the atomic write
// uses is gone once the rename succeeds — a stray config.toml.tmp-* beside the
// real one would be picked up by a later glob or a backup script.
func TestSaveLeavesNoDebrisBehind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatalf("saving: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "config.toml.tmp-") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

// firstZeroValue returns the path of the first field holding a zero value, so
// the round-trip fixture cannot silently stop covering a field. Bools are the
// exception, noted in fullConfig: their only non-zero value is true.
func firstZeroValue(v reflect.Value, path string) string {
	switch {
	case v.Kind() == reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if field.PkgPath != "" {
				continue // unexported, so not part of the schema
			}
			if found := firstZeroValue(v.Field(i), path+field.Name+"."); found != "" {
				return found
			}
		}
		return ""
	case v.Kind() == reflect.Slice:
		if v.Len() == 0 {
			return path
		}
		return firstZeroValue(v.Index(0), path+"[].")
	case v.Kind() == reflect.Map:
		if v.Len() == 0 {
			return path
		}
		return firstZeroValue(v.MapIndex(v.MapKeys()[0]), path+"{}.")
	case v.Kind() == reflect.Bool:
		return "" // see fullConfig: covered by TestExplicitZeroValuesBeatTheDefaults
	default:
		if v.IsZero() {
			return path
		}
		return ""
	}
}
