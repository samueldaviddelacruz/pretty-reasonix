package builtin

import "slices"

// Names lists the tools compiled into this program, by name.
//
// Names rather than values: every one of these needs a workspace root to be constructed, and the root is a
// decision for the layer that wires a run rather than for this package. Listing them here is what lets a
// caller check that what it registered is what was built.
func Names() []string { return slices.Clone(names) }

var names = []string{"read_file", "ls", "glob", "grep"}
