package builtin

import "slices"

// Names lists the tools compiled into this program.
//
// Empty until the first built-ins land (E5-T3 onward). It exists now because the registry's contents are
// a boot-time decision, and a caller that had to know the list itself would make that decision somewhere
// else — which is how two frontends end up with different tool sets.
func Names() []string { return slices.Clone(names) }

var names = []string{}
