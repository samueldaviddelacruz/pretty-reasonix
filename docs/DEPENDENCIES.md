# Dependency policy

Companion to [`TECHNICAL_DESIGN.md`](./TECHNICAL_DESIGN.md) §6.1 and
[`GO-STANDARDS.md`](./GO-STANDARDS.md) §7.

pretty-reasonix ships as **one static binary** built with `CGO_ENABLED=0`. That
makes every dependency a *distribution* decision as much as a coding one: a
single cgo-requiring module, or one that needs a shared library on the target
machine, breaks the install story for everybody. The rule below is therefore
deliberately stricter than "it saved us some code".

---

## 1. The rule

A module may enter this module only if all of the following hold.

1. **Pure Go.** No cgo, no `//go:build cgo` variants required for a supported
   platform. `CGO_ENABLED=0 go build ./...` must stay green (§4).
2. **It earns its place.** The same pull request adds a row to §2 naming one
   concrete reason. "It might be useful" is not a reason.
3. **We could replace it.** If the module disappeared tomorrow, the replacement
   would be bounded work — rule [#5]'s "abstractions should be discovered"
   applied to code we did not write. A dependency we cannot stand behind is a
   dependency we cannot remove.
4. **It is maintained, licensed compatibly, and reasonably sized.** MIT, BSD or
   Apache-2.0. A transitive tree far larger than the feature it provides is a
   refusal, not a footnote.
5. **It does not take a decision we own.** Config parsing, retry policy,
   permission evaluation, event vocabulary and the session format are ours
   (D2). A library may help implement them; it may not define them.
6. **Transitive dependencies count.** Adding X adds everything X requires, and
   every one of them has to satisfy this list too. Check before, not after:
   `go mod graph | wc -l`.

**Standard library first.** The stdlib is preferred even when it is more work,
and `exptostd` in `.golangci.yml` actively pushes test helpers that way. A
dependency has to beat the standard library on more than convenience.

## 2. The approved set

This is the complete list of modules allowed to appear in `go.mod`. Nothing here
is in `go.mod` yet: `go mod tidy` removes a requirement no code imports, so each
row is added by the ticket that first needs it.

| Module | Why it is approved | Why not the alternative |
| --- | --- | --- |
| `charm.land/bubbletea/v2` | The terminal application framework. The stated point of the project. | Hand-rolled terminal input and rendering: weeks of work, and strictly worse. |
| `charm.land/bubbles/v2` | The components the UI is built from: textarea, viewport, spinner. | Reimplementing a text editor widget is a known rabbit hole. |
| `charm.land/lipgloss/v2` | Styling and layout primitives. | Hand-written ANSI escapes in every render function. |
| `charmbracelet/x/ansi` | Escape-sequence handling that the styling layer depends on. | Reimplementing a parser for the terminal's control sequences. |
| `charmbracelet/colorprofile` | Colour-depth detection, so a 256-colour terminal gets intentional colours rather than downsampled ones (TDD §6.12.6). | Guessing the terminal's capability from environment variables. |
| `alecthomas/chroma/v2` | Syntax highlighting, behind an interface with a plain-text fallback (E13-T1). | No mature pure-Go alternative. |
| `yuin/goldmark` | CommonMark plus GFM tables for rendering answers. | A hand-rolled markdown parser is a project of its own. |
| `aymanbagabas/go-udiff` | Unified diffs for the file-mutating tools' results and diff cards. | Hand-rolling Myers diff for a solved problem. |
| `rivo/uniseg` | Grapheme-cluster segmentation, which is what makes width correct for CJK and combining marks. | `go-runewidth` alone miscounts combining sequences. |
| `mattn/go-runewidth` | East Asian width for per-rune terminals, as a fallback beside uniseg. | uniseg alone does not cover the legacy terminal case. |
| `BurntSushi/toml` | Config parsing. | Hand-rolled TOML is a source of subtle bugs; the reference project reached the same conclusion and takes exactly one config dependency. |
| `mvdan.cc/sh/v3` | Parsing a shell command so permission rules match its *structure* rather than its text (TDD §6.7). | Regular expressions over command strings, which is precisely the approach the rule engine exists to avoid. |
| `golang.org/x/sync` | `errgroup` for the parallel tool batch. Required by baseline rule [#73], which names hand-rolled goroutine fan-out with error handling and cancellation as the thing to avoid. | The hand-rolled version [#73] warns about. |
| `go.uber.org/goleak` | Goroutine-leak assertions in `TestMain`, which turn baseline rule [#62] ("every goroutine needs a known exit") from a review rule into a test failure. | Nothing else catches a leak before it becomes a hang. |

## 3. Deliberately refused

Recorded so the same proposal is not re-litigated from scratch.

| Module | Why not |
| --- | --- |
| `modernc.org/sqlite` (or any database) | The reference's SQLite stores are disposable *projections* for search and usage features that are out of v1 (D1). v1 has no database at all (TDD §6.1). Revisit with E20-style catalogs, if ever. |
| `zalando/go-keyring` | Credentials are a `0600` file under `~/.prx` (TDD §6.1). The keyring is a platform-integration liability for no v1 gain. |
| `spf13/cobra`, `urfave/cli` | The subcommand surface is small enough to route by hand, and hand-routing keeps exit codes and flag placement visible in one file (E19-T1). Reconsider only if routing becomes error-prone. |
| A logging framework (`zap`, `zerolog`, …) | Nothing to manage yet. `log/slog` is in the standard library if it becomes necessary. |
| `stretchr/testify` | The standard library's `testing` is sufficient and `exptostd` pushes the same way. |
| `github.com/joho/godotenv` | Reading a `.env` file is a few dozen lines and we already control the format (E2-T5). |
| `tree-sitter` | Used by the reference for `code_index`, which is a deferred tool (Appendix B §R1). |

**Pending a decision, not refused:** `spf13/pflag` if hand-routing flags proves
worse than a dependency at E19-T1, and a TUI-adjacent module if Charm's own set
leaves a gap E10 needs.

## 4. Enforcement

```sh
make pure-go                        # CGO_ENABLED=0 go build ./...
go mod tidy && git diff --exit-code go.mod go.sum
```

- **`make pure-go` is the CGO check.** `make build` only compiles `./cmd/prx`, so
  a cgo-requiring module that nothing imports *yet* would go unnoticed until it
  was wired in. `pure-go` builds every package. CI runs it from E1-T6; the
  target exists now so that ticket has one command to call rather than an
  inlined build.
- **`go mod tidy` must leave no diff.** A requirement that is not imported is
  removed by tidy, so a stale `go.mod` cannot linger.
- **`go.sum` is committed.** It is the record of what was actually approved.

**Not yet mechanical:** nothing asserts that `go.mod`'s require list is a subset
of §2. Parsing `go.mod` in a test would close that gap without a dependency, and
it is worth doing once the list stops being empty — a policy that only prose
enforces is a policy that drifts.

## 5. Review checklist

For any pull request that adds, bumps or removes a dependency:

- [ ] Every new module has a row in §2 with a concrete reason, added in this PR.
- [ ] `make pure-go` passes — the module is pure Go.
- [ ] `go mod tidy` is a no-op and `go.sum` is committed.
- [ ] The module's licence is MIT, BSD or Apache-2.0.
- [ ] The transitive additions were looked at, not assumed.
- [ ] It does not duplicate something an approved module already provides.
- [ ] It does not take a decision we own (D2).
- [ ] If a module was **removed**, its §2 row is removed in the same PR.
- [ ] If the version **bumped**, the reason is in the commit message.
