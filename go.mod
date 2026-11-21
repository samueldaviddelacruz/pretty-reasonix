// Module pretty-reasonix.
//
// The toolchain is pinned by the go directive: Go reads it as a minimum
// toolchain, so with GOTOOLCHAIN=auto the build uses exactly Go 1.27.1. Nothing
// in this module may depend on an older language level — for example the
// per-iteration loop variables introduced in Go 1.22, which rule [#63] of the
// Go practice baseline relies on.
//
// A separate `toolchain` line would be redundant here (and `go mod tidy`
// removes it), so the go directive is the single source of truth.
module pretty-reasonix

go 1.27.1

require (
	charm.land/bubbletea/v2 v2.1.2
	github.com/BurntSushi/toml v1.6.0
	github.com/charmbracelet/x/term v0.2.2
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4 // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/charmbracelet/x/exp/golden v0.1.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.8-0.20261110175422-3a370411ca06 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
