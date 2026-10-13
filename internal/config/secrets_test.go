package config_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pretty-reasonix/internal/config"
)

// A value that looks like a key, so a leak is obvious in a failure message.
const secretValue = "sk-live-abcdef123456"

// TestASecretIsNeverPrinted is the "the file is never printed" half of the
// criterion, expressed where it can actually be checked: on the type.
func TestASecretIsNeverPrinted(t *testing.T) {
	t.Parallel()
	secret := config.NewSecret("TEST_PROVIDER_KEY", secretValue)

	// Every verb fmt can reach the value through, plus the two methods directly.
	// The verbs are a slice rather than literals at each call site, so this tests
	// what fmt does with a Formatter instead of being folded away by a linter into
	// the method call it reduces to.
	verbs := []string{"%v", "%s", "%q", "%x", "%+v", "%#v"}
	printed := make([]string, 0, len(verbs)+2)
	printed = append(printed, secret.String(), secret.GoString())
	for _, verb := range verbs {
		printed = append(printed, fmt.Sprintf(verb, secret))
	}
	for _, text := range printed {
		if strings.Contains(text, secretValue) {
			t.Errorf("a printed secret revealed its value: %s", text)
		}
	}

	// The other half: a secret that could not be read would be useless, so the
	// redaction must be a door rather than a wall.
	if got := secret.Reveal(); got != secretValue {
		t.Errorf("Reveal() = %q, want the value", got)
	}
	if !secret.IsSet() {
		t.Error("IsSet() is false for a secret that has a value")
	}
}

// TestAnUnsetSecretSaysSo keeps the redacted form useful: "not set" is what tells
// a reader to configure the key.
func TestAnUnsetSecretSaysSo(t *testing.T) {
	t.Parallel()
	secret := config.NewSecret("TEST_PROVIDER_KEY", "")
	if secret.IsSet() {
		t.Error("IsSet() is true for an empty value")
	}
	if !strings.Contains(secret.String(), "not set") {
		t.Errorf("String() = %q, want it to say the secret is not set", secret.String())
	}
}

// TestTheEnvFileIsOwnerOnly checks the mode the criterion names. E2-T2's layout
// test only asserts that files carry no execute bits, which 0644 would also pass.
func TestTheEnvFileIsOwnerOnly(t *testing.T) {
	t.Parallel()
	for _, location := range config.Layout {
		if location.Rel != ".env" {
			continue
		}
		if location.Mode.Perm() != 0o600 {
			t.Errorf(".env is created with mode %#o; provider keys live there", location.Mode.Perm())
		}
		return
	}
	t.Fatal(".env is not in config.Layout, so nothing says where provider keys live")
}

// TestCredentialsComeOnlyFromTheGlobalEnvFile reads the one source that may hold
// them, and checks a malformed line is reported rather than silently dropped.
func TestCredentialsComeOnlyFromTheGlobalEnvFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := "# a comment\n\nTEST_PROVIDER_KEY=" + secretValue + "\nOTHER=plain value\nQUOTED=\" with spaces \"\nthis line is prose\n"
	path := writeTemp(t, dir, "env", body)

	credentials, warnings, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got := credentials["TEST_PROVIDER_KEY"].Reveal(); got != secretValue {
		t.Errorf("the key was not read back")
	}
	if got := credentials["OTHER"].Reveal(); got != "plain value" {
		t.Errorf("OTHER = %q, want the plain value", got)
	}
	if got := credentials["QUOTED"].Reveal(); got != " with spaces " {
		t.Errorf("QUOTED = %q, want one layer of quotes removed and the spaces kept", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("want one warning for the prose line, got %d: %v", len(warnings), warnings)
	}
	if warnings[0].Source != path {
		t.Errorf("the warning names %q, want the file it came from", warnings[0].Source)
	}

	// Warnings are printed, so they must not carry a value.
	for _, w := range warnings {
		if strings.Contains(w.String(), secretValue) {
			t.Errorf("a warning leaked a secret: %s", w)
		}
	}
}

// TestAMissingEnvFileIsOrdinary: a user who has configured no provider has not
// written one, and that is not a failure.
func TestAMissingEnvFileIsOrdinary(t *testing.T) {
	t.Parallel()
	credentials, warnings, err := config.LoadCredentials(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("LoadCredentials with no file: %v", err)
	}
	if len(credentials) != 0 || len(warnings) != 0 {
		t.Errorf("got %d credentials and %d warnings, want none of either", len(credentials), len(warnings))
	}
}

// TestAProjectEnvFileCannotSupplyAProviderKey is the second half of the criterion.
func TestAProjectEnvFileCannotSupplyAProviderKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The project defines a reserved name and one it is entitled to define.
	projectEnv := writeTemp(t, dir, "project.env", "TEST_PROVIDER_KEY=stolen\nGITHUB_TOKEN=from-project\n")

	expander, warnings, err := config.ProjectExpander(projectEnv, []string{"TEST_PROVIDER_KEY"})
	if err != nil {
		t.Fatalf("ProjectExpander: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("want one warning for the dropped definition, got %d: %v", len(warnings), warnings)
	}

	// An MCP setting may use the name the project is entitled to define.
	got, attempted := expander.Expand("Bearer ${GITHUB_TOKEN}")
	if got != "Bearer from-project" {
		t.Errorf("${GITHUB_TOKEN} expanded to %q, want the project's own value", got)
	}
	if len(attempted) != 0 {
		t.Errorf("an allowed name was reported as refused: %v", attempted)
	}

	// It may not use a credential: the value does not appear, and the attempt is
	// reported rather than left as a silently empty setting.
	got, attempted = expander.Expand("Bearer ${TEST_PROVIDER_KEY}")
	if strings.Contains(got, "stolen") {
		t.Errorf("a project file supplied a provider key: %q", got)
	}
	if len(attempted) != 1 || attempted[0] != "TEST_PROVIDER_KEY" {
		t.Errorf("the refused name was not reported: %v", attempted)
	}

	// A default is still honoured: it is text the project wrote, not the
	// credential it was not allowed to name.
	if got, _ := expander.Expand("${TEST_PROVIDER_KEY:-none}"); got != "none" {
		t.Errorf("got %q, want the default", got)
	}
	// And an unreserved, undefined name falls back the same way.
	if got, _ := expander.Expand("${NOT_SET_ANYWHERE:-none}"); got != "none" {
		t.Errorf("got %q, want the default for an undefined name", got)
	}
}

// TestAToolChildProcessCannotSeeAProviderKey runs a real child, because the claim
// is about a child process rather than about our own slice arithmetic.
func TestAToolChildProcessCannotSeeAProviderKey(t *testing.T) {
	t.Parallel()
	const name = "TEST_PROVIDER_KEY"
	parent := append(os.Environ(), name+"="+secretValue)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/env")
	cmd.Env = config.ChildEnv(parent, []string{name})
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running /usr/bin/env: %v", err)
	}

	if strings.Contains(string(out), name+"=") {
		t.Errorf("the child environment still carries %s", name)
	}
	if strings.Contains(string(out), secretValue) {
		t.Errorf("the child environment still carries the value")
	}
	// The positive control: a variable that was not scrubbed IS visible, so this
	// test cannot pass by the child seeing no environment at all.
	if !strings.Contains(string(out), "PATH=") {
		t.Error("the child saw no environment at all, so the assertion above proves nothing")
	}
}

// TestProviderKeyNamesComesFromTheConfiguration ties the reserved names to the
// configuration rather than to a hand-kept list.
func TestProviderKeyNamesComesFromTheConfiguration(t *testing.T) {
	t.Parallel()
	cfg, _, err := config.Parse([]byte("[[providers]]\nname = \"vendor\"\nkind = \"openai\"\napi_key_env = \"TEST_PROVIDER_KEY\"\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	names := cfg.ProviderKeyNames()
	if len(names) != 1 || names[0] != "TEST_PROVIDER_KEY" {
		t.Errorf("ProviderKeyNames() = %v, want the configured api_key_env", names)
	}
	if got := config.Default().ProviderKeyNames(); len(got) != 0 {
		t.Errorf("the default configuration reserves %v, want nothing", got)
	}
}
