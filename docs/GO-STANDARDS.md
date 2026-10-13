# Go standards and the practice audit

This is how the rules in **[`go-best-practices.md`](../go-best-practices.md)** are
applied in this repository. That file is the source of rules — 100 numbered
items distilled from *100 Go Mistakes and How to Avoid Them*, with stable
numbers that are meant to be cited (`is [#54] a finding here?`). **This file says
which of them a machine decides and which of them a reviewer decides.**

Read the two together:

| File | Role |
| --- | --- |
| `go-best-practices.md` | The rules. Numbered, cited, never renumbered. |
| `docs/GO-STANDARDS.md` (this) | Enforcement: what the linter decides, what the audit decides, and where we deliberately diverge from a rule. |
| `.golangci.yml` | Tier 1 in code: every entry cites the rule it enforces. |

---

## 1. The two tiers

The baseline is not enforced by good intentions, so it is split by *who can
decide*:

1. **Machine-decided → `.golangci.yml`, run on every commit and in CI.** If a
   linter can decide it, a human should never be asked to. Tier 1 is the
   default answer to "is this rule followed?".
2. **Human-decided → the audit protocol in §4.** Some rules are judgements
   about intent (is this interface speculative? is this abstraction
   discovered?) and no linter can decide them. Those are named, numbered audit
   items in §6 — not vague advice.

A rule that is in neither tier is a bug in this document.

---

## 2. Pinned tooling

| Tool | Version | Where | Purpose |
| --- | --- | --- | --- |
| `golangci-lint` | **v2.14.0** (config schema `"2"`) | `~/go/bin/golangci-lint` locally; `golangci-lint-action` in CI | Tier 1 |
| `gofmt` / `goimports` | with the toolchain | `formatters:` section of `.golangci.yml` | [#16] formatting, import grouping |
| Go toolchain | **1.27.1** (module targets 1.27) | `go.mod` | The language level the rules assume |

```sh
golangci-lint config verify   # validates .golangci.yml against the v2 JSON schema
golangci-lint run             # the gate; must exit 0
go vet ./...                  # [#16] the standard set's own analyzer pass
go test -race ./...           # [#83] scheduled deliberately, see §5
```

**Version discipline.** `golangci-lint` and `.golangci.yml` are bumped together
in one commit. A version bump that changes which rules are machine-decided must
update the mapping in §5/§6 in the same commit — that is the whole point of
writing the mapping down.

---

## 3. What is deliberately strict

Three of these are not in the original rules file, but follow from the design in
`TECHNICAL_DESIGN.md` and are now machine-enforced:

| Enforced | Rule source | Why |
| --- | --- | --- |
| No `fmt.Print*`, `print`, `os.Stdout`/`Stderr`, `os.Exit` outside `cmd/` and `internal/frontend/` | TDD §6.2, not `go-best-practices.md` | Library code returns errors; only the frontends own the process streams and the exit code. Machine-enforcing this is how a two-frontend design stays honest. |
| No `time.Now()` outside the injected clock (tests excepted) | [#87] | Timers are everywhere in a TUI (spinner, flash, elapsed). A clock dependency is what makes those testable without sleeping. |
| No `time.Sleep`, anywhere, including tests | [#86] | A sleep in a test is a design problem, not a fix. |

---

## 4. The audit protocol

Run this **after writing or changing Go code, before declaring the work done** —
it is part of the definition of done, alongside the tests.

1. **Run tier 1.** `golangci-lint run` must exit 0. Fix the code, not the config.
2. **Diff the change** and read it as a reviewer: which packages changed, what
   new API surface, what new concurrency, what new allocation on a hot path.
3. **Walk §6.** It is grouped so you can read only the groups your diff touches.
   Cite findings as the rule number plus the location: `[#25] internal/session/store.go:118`.
4. **Check the false-positive rule first.** The rules file is explicit: *"Before
   reporting a violation, check that the code doesn't already satisfy the rule a
   few lines away."* The most common false positive is flagging work already
   done, and a report padded with those is a report nobody finishes.
5. **Respect the perf tag.** Rules in the file marked *(perf)* — the whole of
   §12 there — only matter on a measured hot path. Never trade readability for
   one without a benchmark. A §12 finding without a number attached is not a
   finding.
6. **Report, don't silently diverge.** If a rule conflicts with this codebase's
   established style, §5's instruction applies: follow the codebase and flag the
   conflict. Silently diverging is the one outcome that is never acceptable.
7. **Record the outcome.** Each finding is either fixed, or accepted with a
   one-line reason and a ticket id if it needs real work. "Accepted, no reason"
   is not an outcome.

**Where the audit is recorded.** On the PR: the tier-1 result, the §6 groups
walked, and the findings — fixed or accepted. A change with no audit record is
not reviewable and should not be merged.

---

## 5. Rule → enforcement map (machine-decided)

Tier 1. Generated by hand from `.golangci.yml`; keep the two in step.

| Rule | Enforced by |
| --- | --- |
| [#1] don't shadow by accident | `govet` (`shadow`), `wastedassign`, `reassign` |
| [#2] avoid unnecessary nesting | `nestif`, `revive` (`indent-error-flow`, `superfluous-else`, `early-return`) |
| [#3] avoid `init` functions | `gochecknoinits` |
| [#7] return concrete types, not interfaces | `ireturn` (+ `revive:unexported-return`) |
| [#5]/[#6] no speculative interfaces; keep them small | `interfacebloat` (`max: 6`) |
| [#12] don't over-package early | `decorder` (declaration order per file); package *count* is an audit item |
| [#13] no `utils`/`common`/`base` packages | `depguard` |
| [#14] no collisions with package names or builtins | `revive` (`var-naming`, `redefines-builtin-id`), `predeclared`, `gocritic` (`importShadow`) |
| [#15] document every exported element | `revive` (`exported`, `package-comments`), `godot` |
| [#16] always lint and format | `formatters: [gofmt, goimports]` + `golangci-lint run` in CI |
| [#17] octal literals written explicitly | `gocritic` (`octalLiteral`) |
| [#18] integer overflow is silent | `gosec` (G115) |
| [#21] preallocate slices when the size is known | `prealloc` |
| [#25] `append` can mutate a shared backing array | `makezero` (the append-to-non-zero-length case) |
| [#27] preallocate maps too | `prealloc` |
| [#30] the range loop's value is a copy | `gocritic` (`rangeValCopy`), `intrange` |
| [#35] don't `defer` inside a loop | `gocritic` (`deferInLoop`) |
| [#39] `strings.Builder` for concatenation in loops | `perfsprint` (partial — see §6) |
| [#40] don't bounce between `string` and `[]byte` | `mirror`, `gocritic` (`stringXbytes`), `unconvert` |
| [#42] pointer receivers where they are warranted | `recvcheck`, `revive` (`receiver-naming`, `unused-receiver`) |
| [#43] named results sparingly, no naked returns | `nakedret`, `nonamedreturns` |
| [#44] named results are zero-initialized and mutable | `wastedassign` |
| [#45] a nil pointer in an interface is not a nil interface | `nilnil`, `nilerr` |
| [#48] `panic` is for programmer errors | `forbidigo` (`panic` allowed only under `cmd/`) |
| [#49] `%w` vs `%v` chosen deliberately | `errorlint`, `errname`, `revive` (`error-strings`, `error-naming`) |
| [#50] check error types with `errors.As` | `errorlint` |
| [#51] compare error identity with `errors.Is` | `errorlint` |
| [#53] never swallow an error implicitly | `errcheck` (standard set) + `nolintlint` (`require-explanation`) |
| [#53] nothing left silently undone: no `BUG`/`FIXME` ships | `godox` |
| [#54] deferred `Close`/`Flush` can fail | `errcheck`, `bodyclose`, `sqlclosecheck` |
| [#9]/[#8] `any` and type parameters only where they carry meaning | `forcetypeassert` (the concrete risk at the `any` boundary); the design question is §6 |
| [#60] understand `context.Context` | `containedctx`, `revive` (`context-keys-type`) |
| [#61] propagate contexts deliberately | `containedctx`, `contextcheck`, `fatcontext`, `revive` (`context-as-argument`) |
| [#62] every goroutine needs a known exit | `contextcheck` (partial); the loop-exit proof is §6 |
| [#63] the loop variable trap | `copyloopvar`, `govet` (`loopclosure`) |
| [#67] unbuffered by default | Not enforceable — §6 lists the two sanctioned buffers |
| [#74] never copy a type containing a sync value | `govet` (`copylocks`) |
| [#75] always use the `time` constants | `usestdlibvars`, `durationcheck` |
| [#77] watch the JSON traps | `errchkjson`, `musttag` |
| [#78] `sql.Open` does not connect | `sqlclosecheck`, `rowserrcheck` (resource handling only) |
| [#79] close every transient resource | `bodyclose`, `sqlclosecheck`, `rowserrcheck`, `gocritic` (`deferInLoop`) |
| [#80] `return` after replying to an HTTP request | `canonicalheader` only — the missing `return` itself is §6 |
| [#81] never ship the default HTTP client or server | `forbidigo` (`http.DefaultClient`, `http.DefaultServeMux`), `gosec` (G112), `noctx` |
| [#82] categorize tests | `run.build-tags: [integration]` + the tier tags in §7 |
| [#84] use the test execution flags | `tparallel`, `paralleltest` |
| [#85] table-driven tests | Not enforceable (`copyloopvar` covers the loop-variable half) |
| [#86] never `time.Sleep` to fix a flaky test | `forbidigo` (`time.Sleep`), `usetesting` |
| [#87] don't call `time.Now()` deep inside untestable code | `forbidigo` (`time.Now`), `usetesting` |
| [#88] use the testing utility packages | `exptostd` |
| [#90] know the testing toolkit | `testpackage`, `thelper`, `tparallel`, `usetesting` |
| Complete switches over enums | `exhaustive` (`ChunkKind`, `Decision`, `RowKind`, `TurnStatus`, `TaskState`, `Phase`) |
| Current idioms for the Go version we target | `modernize` |
| Spelling and bidirectional text | `misspell`, `bidichk`, `dupword`, `asasalint` |

---

## 6. Audit checklist (human-decided)

The rules no linter can decide. Walk the groups your diff touches. Rule numbers
are the citation form.

### 6.1 Abstraction and API shape — [#4] [#5] [#6] [#8] [#9] [#10] [#11] [#12] [#46]
- [#5] Is this interface *discovered* rather than designed up front? Would the
  code be simpler with a concrete type?
- [#6] Is the interface defined where it is *consumed*? (The one sanctioned
  exception is `internal/contract`, see §7.)
- [#7] Does a constructor return an interface it did not need to?
- [#4] Does a getter merely relay a field? If it must exist, is it `Balance()`
  and not `GetBalance()`?
- [#8] Does `any` say something? If it is a kind-specific options bag, is it
  documented as such, with a typed accessor at the call site?
- [#9] Did a type parameter remove real duplication, or just add ceremony?
- [#10] Is an embedded type promoting fields into our public API by accident?
- [#11] Optional configuration: functional options, not a config struct full of
  zero-value traps?
- [#12] Is a new package justified, or is this a file in an existing one?
- [#46] Does a function accept an `io.Reader`/`io.Writer` where it could, or
  does it open a file and become untestable in memory?

### 6.2 Types, slices, maps — [#19] [#20] [#22] [#23] [#24] [#26] [#28] [#29] [#36] [#37] [#38] [#39] [#41]
- [#19] Any `==` on floats, or an expression whose result depends on operand
  order?
- [#20] Is `len`/`cap` used deliberately? Is `cap` relied on anywhere?
- [#22] Returning `[]T{}` where `var s []T` would do?
- [#23] Comparing a slice to `nil` instead of checking `len(s) == 0`?
- [#24] Is a `copy` destination sized as `len(src)` on purpose?
- [#26]/[#41] Does a small slice (or substring) pin a large backing array? Is a
  long-lived pointer field left set on something we no longer need?
- [#28] Did a map grow large and then shrink, keeping its buckets?
- [#29] Is `reflect.DeepEqual` (or a purpose-built comparison) used rather than a
  hand-rolled one — and was the standard library checked first?
- [#36]/[#37] Rune vs byte: is `len(s)` being read as a character count? Is
  `s[i]` being used as a rune index?
- [#38] `TrimRight`/`TrimLeft` (a set) vs `TrimSuffix`/`TrimPrefix` (one exact
  string)?
- [#39] String concatenation inside a loop without a `strings.Builder`?

### 6.3 Control flow and guards — [#31] [#32] [#33] [#34] [#47]
- [#31] Is the ranged expression assumed to be re-evaluated per iteration?
- [#32] Is `&v` taken from a range variable?
- [#33] Does anything depend on map iteration order?
- [#34] Does a `break` intend to exit an enclosing loop but exit only the
  innermost `switch`/`select`?
- [#47] Does a `defer` need the *later* value of a variable (closure) rather than
  its value at the `defer` statement?

### 6.4 Errors and resources — [#52]
- [#52] Is an error logged *and* returned? One or the other, exactly once.
- [#54] Does a deferred `Close`/`Flush` silently overwrite the error we meant to
  return?

### 6.5 Concurrency — [#55] [#56] [#57] [#58] [#59] [#62] [#64] [#65] [#66] [#67] [#68] [#69] [#70] [#71] [#72] [#73]
- [#55]/[#56] Is concurrency here *structure*, and was the concurrent version
  measured against the sequential one?
- [#57] Channels to signal and transfer ownership, mutexes to guard state —
  chosen per problem?
- [#58] Is this a genuine race condition rather than a data race? Are the
  happens-before edges established by channels, atomics or a mutex?
- [#59] Is the goroutine count derived from the workload (CPU-bound vs
  I/O-bound) and validated?
- [#62] Every goroutine: what is its exit, and who cancels it? (Goroutine leaks
  are caught by a `goleak` check in `TestMain` — see §7.)
- [#64] Does anything rely on `select` preferring an earlier case?
- [#65] A data-free channel as `chan struct{}`; a set as `map[K]struct{}`?
- [#66] Is a nil channel used deliberately to disable a `select` case?
- [#67] Is a buffer larger than 1 justified, measured and commented? Only the
  two buffers in §7 are sanctioned today.
- [#68] Is a `String()`/`%v` reached on a value that is concurrently mutated or
  holds a lock?
- [#69] Is a slice appended to from more than one goroutine — even "its own"
  element? Collect by index into a pre-sized slice, or pass values over a
  channel.
- [#70] Does a mutex cover every read *and* write of the data it protects, with
  no reference escaping to be mutated outside the lock?
- [#71] `wg.Add` in the parent before the child starts; `Done` inside the child?
- [#72] Is `sync.Cond` carrying a genuinely arbitrary condition, waited in a
  loop?
- [#73] Is this a hand-rolled goroutine fan-out with error handling and
  cancellation where `errgroup` would do?

### 6.6 Standard library — [#76] [#80]
- [#76] `time.After` inside a loop or long-lived `select` (its timer is not
  collected until it fires) instead of `time.NewTimer` + `defer Stop()`?
- [#80] Does an HTTP handler `return` after `http.Error`/`w.Write`?

### 6.7 Tests — [#85] [#89]
- [#85] Table-driven where cases share a structure? (The `tt := tt` half is
  handled by `copyloopvar`.)
- [#89] Is the benchmark correct: `b.ResetTimer()` around setup, inputs
  recreated per iteration, the work impossible to optimize away, and
  `b.ReportAllocs()` where allocation matters?

### 6.8 Optimization — [#91]–[#100] — *perf: measure first*
Every rule here needs a number before it is a finding (`[#56]`, `[#89]`).
- [#91] Access patterns and locality: is a micro-benchmark number from this
  machine being used to justify a readability cost?
- [#92] False sharing between goroutines writing to the same cache line?
- [#93] Is independent work structured so the CPU can overlap it?
- [#94] Struct field order: is this a measured-hot struct? (The
  `fieldalignment` analyzer is available in `govet` and deliberately off.)
- [#95] Is a pointer being returned "to avoid a copy" without escape-analysis
  evidence?
- [#96] Is a buffer being reused (caller-provided) or a `sync.Pool` warranted —
  and is the pool not being used as a cache?
- [#97] Is the hot path small enough to inline, with the slow path split out?
- [#98] Would `pprof`/the execution tracer answer this faster than reasoning?
- [#99] `GOGC`/`GOMEMLIMIT` tuned with the heap peak understood?
- [#100] Is the process CPU-quota aware (`GOMAXPROCS` sees host CPUs, not a
  container quota)?

---

## 7. Deliberate divergences and sanctioned exceptions

Where the design in `TECHNICAL_DESIGN.md` and a rule disagree, the rule is
followed unless it is listed here — with the reason, as §5 requires.

| Rule | Divergence | Reason |
| --- | --- | --- |
| **[#3] no `init`** | **The design previously adopted the reference's `init()` self-registration for providers and tools. That is removed: registration is explicit, called from `internal/assembly`.** | [#3] is right and this is a greenfield codebase, so there is no legacy to preserve. Explicit wiring also removes blank imports that exist only for side effects, makes registration order visible, lets a duplicate registration return an error instead of a panic, and simplifies the architecture test. See TDD §6.2/§6.3. |
| **[#7] return concrete types** | `internal/contract` defines `Provider`, `Tool`, `Sink`, `Gate`, `Approver`, `Store`, `Runner`, `Overlay`, and constructors return them. They are in `ireturn`'s allow list. | These are the up-front abstractions [#7] itself exempts (like `io.Reader`), defined once and consumed by several implementations, which is also what [#6] asks for when there is more than one consumer. Every *other* constructor in the codebase returns a concrete type, which is what makes the exception list meaningful. |
| **[#8] avoid `any`** | `provider.Config.Extra map[string]any` stays. | A provider kind genuinely has kind-specific options. It is the **single sanctioned `any` bag**: `forcetypeassert` forbids bare assertions at the call site, so every read goes through a typed accessor that can report a wrong type instead of panicking. |
| **[#67] unbuffered channels by default** | Two buffers are sanctioned: the TUI event sink and the transcript row-update queue. | The engine must never block on a slow frontend, so back-pressure in the other direction is the wrong trade. Both are **named constants with a comment** stating the size and the drop policy, and dropping surfaces a counted notice rather than silent loss. A third buffer needs its own entry here and a measurement. |
| [#73] use `errgroup` | Positive obligation, not a divergence: the parallel tool batch in the turn loop uses `golang.org/x/sync/errgroup` with results placed **by index into a pre-sized slice** ([#69]). | Adds `golang.org/x/sync` as a dependency — recorded in `docs/DEPENDENCIES.md` per E1-T4. It is a small, pure-Go, standard-adjacent module and it replaces exactly the hand-rolled fan-out [#73] warns about. |
| [#62] known goroutine exit | `TestMain` runs a `goleak` check in every package with concurrency. | Turns a code-review rule into a test failure. `go.uber.org/goleak` is already the reference implementation's choice for the same reason. |
| [#15] document every exported element | Applied strictly, including `internal/` packages. | The TUI and the contract packages are the documentation surface for contributors; the cost is one sentence per symbol. |
| [#48] `panic` only for programmer errors | In practice `panic` is confined to `cmd/`. | Registration duplicates and unreadable required configuration surface as errors, not panics, so library code has no legitimate panic. `cmd` is where "the world is broken, we cannot start" belongs. |
| [#82] categorise tests | Three tiers by build tag: untagged (unit), `integration`, `e2e`. | Fast unit runs on every commit; `integration` and `e2e` scheduled; `-race` [#83] applied to the unit tier plus a scheduled full run rather than unconditionally, per [#83]'s own caveat about cost. |

## 8. Keeping this honest

- **A new rule past 100** in `go-best-practices.md` gets an entry here: either a
  linter in §5 or an item in §6. A rule in neither tier is an unenforced rule.
- **A `golangci-lint` bump** re-runs §5: confirm every enforcer still exists
  under its current name (the tool deprecates and renames linters between major
  versions — v2 alone renamed `wsl`, `exhaustruct` and `gomodguard`).
- **A rule found to be unenforceable** moves to §6 explicitly rather than
  silently disappearing.
- **A divergence** gets a row in §7 naming the rule and the reason. Divergence
  is allowed; undocumented divergence is not.
