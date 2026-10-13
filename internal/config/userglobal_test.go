package config_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
)

// TestProjectCannotSetUserGlobalKeys is E2-T4's acceptance criterion.
//
// It iterates the declared list rather than a copy of it, so a key added to
// config.UserGlobalKeys is covered the moment it is declared — and fails loudly
// if this test has no sample value for it. An entry nobody checks is a rule the
// program claims and does not keep.
func TestProjectCannotSetUserGlobalKeys(t *testing.T) {
	t.Parallel()

	// samples gives each declared key a value to attempt. The value only has to
	// differ from the default, so that "not applied" is a real observation.
	samples := map[string]string{
		"ui.commandmode": `"vi"`,
	}

	for _, key := range config.UserGlobalKeys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			value, ok := samples[key]
			if !ok {
				t.Fatalf("%s is declared user-global-only but this test has no sample value for it, "+
					"so nothing checks that a project cannot set it", key)
			}
			section, leaf, cut := strings.Cut(key, ".")
			if !cut {
				t.Fatalf("%s is not a section.key path, which is the shape this test builds files from", key)
			}
			body := fmt.Sprintf("[%s]\n%s = %s\n", section, leaf, value)

			dir := t.TempDir()
			project := writeTemp(t, dir, "project.toml", body)

			// 1. Not applied. With no user file, the effective configuration must
			// be exactly the defaults: the project named the key and changed
			// nothing.
			cfg, warnings, err := config.Load(project, "", config.Flags{})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(cfg, config.Default()) {
				t.Errorf("a project file setting %s changed the effective configuration:\ngot  %#v\nwant %#v",
					key, cfg, config.Default())
			}

			// 2. Reported, naming the key and the file to edit.
			reported := 0
			for _, w := range warnings {
				if w.Key == key && w.Source == project {
					reported++
				}
			}
			if reported != 1 {
				t.Errorf("want exactly one warning naming %s from %s, got %d in %v",
					key, project, reported, warnings)
			}

			// 3. The positive control. The same value from the user's own file is
			// obeyed, which is what makes this a rule about projects rather than a
			// test that would pass on a setting nothing reads.
			fromUser, _, err := config.Load("", writeTemp(t, dir, "user.toml", body), config.Flags{})
			if err != nil {
				t.Fatalf("Load from the user's file: %v", err)
			}
			if reflect.DeepEqual(fromUser, config.Default()) {
				t.Errorf("%s had no effect even from the user's own configuration, "+
					"so this test would pass on a key nothing reads", key)
			}
		})
	}
}

// TestUserGlobalKeysAreDeclaredWhereTheRulesLive is a small guard on the list
// itself: an empty or unhelpfully named entry would leave the rule above
// asserting nothing.
func TestUserGlobalKeysAreDeclaredWhereTheRulesLive(t *testing.T) {
	t.Parallel()
	if len(config.UserGlobalKeys) == 0 {
		t.Fatal("UserGlobalKeys is empty, so nothing is held back from a project file")
	}
	for _, key := range config.UserGlobalKeys {
		if strings.Contains(key, " ") || key != strings.TrimSpace(key) {
			t.Errorf("UserGlobalKeys entry %q is not a TOML key path", key)
		}
	}
}
