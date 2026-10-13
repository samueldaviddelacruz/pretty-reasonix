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

require github.com/BurntSushi/toml v1.6.0
