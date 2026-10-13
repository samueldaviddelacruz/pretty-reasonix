// Package config loads, validates and resolves the TOML configuration, and owns
// the paths, roots and identity constants for the whole program.
//
// The schema here is the one documented in docs/TECHNICAL_DESIGN.md §6.11, field
// for field. Every field carries its default in a comment, and Default returns
// exactly those, because a default that is only described in prose is a default
// nobody can check.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrMalformed reports a file that could not be parsed. It is a sentinel so that
// a caller which must not overwrite the file the user wrote — the quarantine
// and repair path in E2-T7, and setup in E19-T2 — can match on it rather than on
// a message (rules [#50], [#51]).
var ErrMalformed = errors.New("malformed configuration")

// Config is the whole configuration file.
type Config struct {
	// DefaultModel names the provider, or provider/model, a session starts with.
	DefaultModel string `toml:"default_model"` // "deepseek"
	// Language selects the UI language. Empty means detect from $LANG / $PRX_LANG.
	Language string `toml:"language"` // ""

	UI          UI          `toml:"ui"`
	Agent       Agent       `toml:"agent"`
	Providers   []Provider  `toml:"providers"`
	Tools       Tools       `toml:"tools"`
	Permissions Permissions `toml:"permissions"`
	Sandbox     Sandbox     `toml:"sandbox"`
	Skills      Skills      `toml:"skills"`
	Environment Environment `toml:"environment"`
	Plugins     []Plugin    `toml:"plugins"`
}

// UI holds presentation settings.
type UI struct {
	// Theme is the terminal background mode, and nothing else.
	Theme string `toml:"theme"` // "auto" | "dark" | "light"
	// ThemeStyle names an accent palette; empty takes the mode's default.
	ThemeStyle string `toml:"theme_style"` // ""
	// CursorShape is the composer cursor; an unknown value falls back to "bar".
	CursorShape string `toml:"cursor_shape"` // "bar" | "block" | "underline"
	// ShowTurnUsage prints the per-request token and cost receipt.
	ShowTurnUsage bool `toml:"show_turn_usage"` // true
	// CommandMode enables the optional vi editing mode (E12-T3).
	CommandMode string `toml:"commandmode"` // "" | "vi"
	// StatusLine selects how much the footer shows (U4).
	StatusLine string `toml:"statusline"` // "compact" | "full" | "off"
}

// Agent holds the loop's own settings.
type Agent struct {
	// SystemPrompt replaces the built-in prompt. SystemPromptFile is the same
	// thing read from disk, and wins when both are set.
	SystemPrompt     string `toml:"system_prompt"`      // ""
	SystemPromptFile string `toml:"system_prompt_file"` // ""
	// Temperature is passed through to the provider.
	Temperature float64 `toml:"temperature"` // 0.0
	// ReasoningLanguage selects the language of visible reasoning text, which is
	// a separate concern from the UI language above.
	ReasoningLanguage string `toml:"reasoning_language"` // "auto" | "en" | "zh"
	// CompactRatio is the sole automatic compaction trigger: the fraction of the
	// context window at which history is folded (§6.9).
	CompactRatio float64 `toml:"compact_ratio"` // 0.85
	// MaxOutputTokens is a per-turn completion ceiling. 0 means the automatic
	// ladder; a negative value omits the wire limit where the protocol allows.
	MaxOutputTokens int `toml:"max_output_tokens"` // 0
	// OutputStyle selects an answer persona.
	OutputStyle string `toml:"output_style"` // ""
	// Keymap overrides the default bindings, action name to key sequence (U1).
	Keymap map[string]string `toml:"keymap"` // {}
}

// Provider is one vendor endpoint, which may offer several models.
type Provider struct {
	Name string `toml:"name"`
	// Kind selects the wire protocol, and is the only thing that makes a vendor
	// a config entry rather than a code change (§6.3).
	Kind    string `toml:"kind"`     // "openai" | "anthropic" | "responses"
	BaseURL string `toml:"base_url"` // ""
	// Models lists what this endpoint offers; Default names one of them.
	Models  []string `toml:"models"`  // []
	Default string   `toml:"default"` // "" means Models[0]
	// APIKeyEnv names the environment variable holding the key. The key itself
	// lives in ~/.prx/.env, never here (E2-T5).
	APIKeyEnv string `toml:"api_key_env"` // ""
	// ContextWindow is the provider-wide fallback in tokens; 0 disables
	// compaction for this provider.
	ContextWindow int `toml:"context_window"` // 0
	// RequestURL, when set, is the exact request target rather than one derived
	// from BaseURL.
	RequestURL string `toml:"request_url"` // ""
	// MaxOutputTokens overrides Agent.MaxOutputTokens for this provider.
	MaxOutputTokens int `toml:"max_output_tokens"` // 0
	// IdleTimeoutSeconds is the stream watchdog: how long a call may send
	// nothing before it is read as dropped.
	IdleTimeoutSeconds int `toml:"idle_timeout_seconds"` // 300
	// ModelOverrides replaces provider-wide settings for one model.
	ModelOverrides map[string]ModelOverride `toml:"model_overrides"` // {}
	// Prices carries per-million-token prices, keyed by model ID.
	Prices map[string]Price `toml:"prices"` // {}
}

// ModelOverride replaces a provider-wide setting for a single model.
//
// The nested shape is provisional: E3-T10 owns model resolution, windows and
// cost, and will extend this rather than treat it as settled.
type ModelOverride struct {
	ContextWindow   int `toml:"context_window"`
	MaxOutputTokens int `toml:"max_output_tokens"`
}

// Price is a per-million-token price in the provider's own billing currency.
//
// Provisional for the same reason as ModelOverride.
type Price struct {
	Input    float64 `toml:"input"`
	Output   float64 `toml:"output"`
	CacheHit float64 `toml:"cache_hit"`
	Currency string  `toml:"currency"`
}

// Tools selects which tools exist and bounds how they behave.
type Tools struct {
	// Enabled lists built-in tool names; empty means all of them.
	Enabled []string `toml:"enabled"` // [] = all built-ins
	// BashTimeoutSeconds is the foreground safety cap; 0 removes the tool-local
	// cap and leaves only the host's.
	BashTimeoutSeconds int `toml:"bash_timeout_seconds"` // 120
	// ProtectChangedFiles refuses a whole-file write over a change made since
	// the agent last read or wrote the file.
	ProtectChangedFiles bool `toml:"protect_changed_files"` // true
	// MCPStartupTimeoutSeconds bounds initialize plus tools/list.
	MCPStartupTimeoutSeconds int `toml:"mcp_startup_timeout_seconds"` // 30
	// MCPCallTimeoutSeconds is the default per-call MCP cap; a server entry may
	// raise it.
	MCPCallTimeoutSeconds int `toml:"mcp_call_timeout_seconds"` // 300
}

// Permissions is the policy layer: what may run, what must be asked about, and
// what may never run (§6.7). Precedence is deny, ask, allow, then Mode.
type Permissions struct {
	// Mode is the writer fallback when no rule matches.
	Mode string `toml:"mode"` // "ask" | "allow" | "deny"
	// Deny, Allow and Ask are rules in the grammar of internal/contract/permrule.
	// The defaults are empty: a shipped configuration should not carry rules the
	// user did not write, and the sample entries in §6.11 are illustrative.
	Deny  []string `toml:"deny"`  // []
	Allow []string `toml:"allow"` // []
	Ask   []string `toml:"ask"`   // []
}

// Sandbox is the enforcement layer beneath Permissions (§6.8): what a tool can
// actually reach, whatever the policy says.
type Sandbox struct {
	// WorkspaceRoot confines the file writers; empty means the process working
	// directory.
	WorkspaceRoot string `toml:"workspace_root"` // "" = cwd
	// AllowWrite lists extra directories the file writers may modify.
	AllowWrite []string `toml:"allow_write"` // []
	// ForbidRead lists paths the read, list and search tools may not inspect,
	// with ${VAR} expansion. Empty by default: a default that silently denied a
	// path would be surprising, and the setup path (E19-T2) can seed the
	// familiar ${HOME}/.ssh where the user sees it being written.
	ForbidRead []string `toml:"forbid_read"` // []
	// Bash is the shell jail mode. "enforce" with no OS backend refuses to run
	// shell rather than running it unconfined — the honest failure.
	Bash string `toml:"bash"` // "enforce" | "off"
	// Network permits network access from inside the sandbox.
	Network bool `toml:"network"` // true
}

// Skills configures where skills are found and which are hidden.
type Skills struct {
	// Paths adds skill roots beyond the conventions.
	Paths []string `toml:"paths"` // []
	// DisabledSkills hides skills from the prompt, the slash surface and the
	// skill tools without deleting the files.
	DisabledSkills []string `toml:"disabled_skills"` // []
}

// Environment controls the startup summary injected into the prompt.
type Environment struct {
	// Enabled injects a stable summary of OS, shell and toolchain versions.
	Enabled bool `toml:"enabled"` // true
	// Offline reports that outbound network access is unavailable, so retries
	// that cannot succeed are not attempted.
	Offline bool `toml:"offline"` // false
}

// Plugin is one MCP server.
type Plugin struct {
	Name    string   `toml:"name"`
	Type    string   `toml:"type"`    // "stdio" | "http"
	Command string   `toml:"command"` // stdio only
	Args    []string `toml:"args"`    // stdio only
}

// Warning is something the file said that this version does not act on. Unknown
// keys are warnings rather than errors so that a configuration written for a
// later version still starts, minus what this one cannot understand.
type Warning struct {
	// Key is the dotted path of the ignored setting, e.g. "ui.theme_packs".
	Key string
	// Note says why it was ignored, in a form fit for `prx doctor`.
	Note string
}

// String renders the warning for a human reader.
func (w Warning) String() string {
	return w.Key + ": " + w.Note
}

// Default returns the documented default configuration. Every value here is the
// one named in the field's comment and in docs/TECHNICAL_DESIGN.md §6.11.
func Default() *Config {
	return &Config{
		DefaultModel: "deepseek",
		Language:     "",
		UI: UI{
			Theme:         "auto",
			ThemeStyle:    "",
			CursorShape:   "bar",
			ShowTurnUsage: true,
			CommandMode:   "",
			StatusLine:    "compact",
		},
		Agent: Agent{
			SystemPrompt:      "",
			SystemPromptFile:  "",
			Temperature:       0.0,
			ReasoningLanguage: "auto",
			CompactRatio:      0.85,
			MaxOutputTokens:   0,
			OutputStyle:       "",
			Keymap:            map[string]string{},
		},
		Providers: nil,
		Tools: Tools{
			Enabled:                  []string{},
			BashTimeoutSeconds:       120,
			ProtectChangedFiles:      true,
			MCPStartupTimeoutSeconds: 30,
			MCPCallTimeoutSeconds:    300,
		},
		Permissions: Permissions{
			Mode:  "ask",
			Deny:  []string{},
			Allow: []string{},
			Ask:   []string{},
		},
		Sandbox: Sandbox{
			WorkspaceRoot: "",
			AllowWrite:    []string{},
			ForbidRead:    []string{},
			Bash:          "enforce",
			Network:       true,
		},
		Skills: Skills{
			Paths:          []string{},
			DisabledSkills: []string{},
		},
		Environment: Environment{
			Enabled: true,
			Offline: false,
		},
		Plugins: nil,
	}
}

// Parse decodes a configuration from TOML bytes.
//
// The defaults are the starting point, so a file only has to name what it wants
// to change. It reads bytes and never writes any, which is what makes it safe to
// call on a file that may be malformed: nothing it does can alter what the user
// wrote.
func Parse(body []byte) (*Config, []Warning, error) {
	cfg := Default()
	meta, err := toml.Decode(string(body), cfg)
	if err != nil {
		// Both errors stay matchable: a caller may want the sentinel, and a
		// human wants the parser's position.
		return nil, nil, errors.Join(ErrMalformed, fmt.Errorf("parsing configuration: %w", err))
	}
	if undecoded := undecodedKeys(meta); len(undecoded) > 0 {
		warnings := make([]Warning, 0, len(undecoded))
		for _, key := range undecoded {
			warnings = append(warnings, Warning{
				Key:  key.String(),
				Note: "this version does not use this setting; it was ignored rather than applied",
			})
		}
		return cfg, warnings, nil
	}
	return cfg, nil, nil
}

// undecodedKeys lists what the file said and the schema did not recognise, in a
// stable order so a warning list does not shuffle between runs.
func undecodedKeys(meta toml.MetaData) []toml.Key {
	keys := meta.Undecoded()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	return keys
}

// LoadFile reads one configuration file.
//
// A missing file is reported as an error wrapping fs.ErrNotExist, so the caller
// decides whether that is fatal — E2-T3 owns which files must exist and in what
// order they are consulted.
func LoadFile(path string) (*Config, []Warning, error) {
	// The path is the file the caller asked this loader to read, so there is no
	// untrusted input to validate it against.
	body, err := os.ReadFile(path) //nolint:gosec // G304: the path is caller-supplied by design
	if err != nil {
		return nil, nil, fmt.Errorf("reading configuration %s: %w", path, err)
	}
	return Parse(body)
}

// Save writes the configuration to path.
//
// It writes a temporary file in the same directory and renames it into place, so
// an interrupted write cannot leave a half-written configuration behind. Callers
// that must not overwrite a file that failed to parse are responsible for
// checking first: the load path never writes, and this one never reads.
func Save(path string, cfg *Config) error {
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encoding configuration: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file beside %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best effort: if the rename succeeded this is already gone, and if it
		// did not, leaving the temporary behind is better than losing the error.
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(buf.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	// The configuration may name an api_key_env, so it is owner-only by default.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restricting %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Missing reports whether an error from LoadFile is the absence of a file rather
// than a problem with one.
func Missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
