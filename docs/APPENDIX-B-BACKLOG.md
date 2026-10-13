# Appendix B — Backlog: milestones, epics and tickets

Companion to [`TECHNICAL_DESIGN.md`](./TECHNICAL_DESIGN.md). This is the **file the GitHub project board is populated from**: one ticket per bullet, one epic per section, one milestone per group.

**160 v1 tickets across 22 epics and 6 milestones**, plus a roadmap tier (§R1) that records every deferred subsystem with a stub ticket, so deferral is explicit rather than forgotten.

---

## B0. How to use this file

### Ticket IDs

`E<epic>-T<n>` — e.g. `E5-T8`. IDs are **stable and never reused**: a ticket that is split becomes `E5-T8a`/`E5-T8b`; a ticket that is dropped is struck through with a reason in place, never deleted.

### Labels

| Label | Values |
| --- | --- |
| `epic:e<n>` | `epic:e1` … `epic:e22` |
| `milestone:m<n>` | `milestone:m0` … `milestone:m5` |
| `area:<x>` | `area:core` `area:config` `area:provider` `area:agent` `area:tools` `area:perm` `area:sandbox` `area:context` `area:state` `area:tui` `area:render` `area:theme` `area:cli` `area:ext` `area:quality` `area:docs` `area:release` |
| `type:<x>` | `type:feature` `type:chore` `type:spike` `type:docs` `type:test` |
| `size:<x>` | `size:S` (≤ half a day) `size:M` (1–3 days) `size:L` (3+ days, should probably be split) |
| `risk:<x>` | `risk:security` `risk:perf` `risk:compat` `risk:data-loss` |
| `good-first-issue` | Applied to the E1/E2/E13-T4/E20 set — self-contained, low blast radius |
| `blocked-by:external` | Needs a real MCP server, a real model endpoint, a sandbox backend, or another decision |

### Ticket format

```
- **`E<epic>-T<n>` · Short imperative title** — `size:S|M|L` · deps: `<ticket ids>` · labels: `<labels>`
  - AC: one testable acceptance criterion.
  - AC: another, where the ticket has two independent halves.
```

An AC is only written when it is checkable. "Works well" is never an AC.

### Ordering rule

Within a milestone, work items are ordered by **what unblocks the most other tickets**, not by user-visible glamour. M2 (the TUI) is the most visible milestone and deliberately *not* first — it is unbuildable without M0/M1's event stream and tool results.

---

## B1. Milestones

| M | Name | Exit criteria (all must hold) | Epics |
| --- | --- | --- | --- |
| **M0** | Walking skeleton | `prx -p "read main.go and summarise it"` completes against a real provider; one session exists on disk with a valid event log; a test suite runs in CI on two platforms. | E1, E2, E3, E4, E5 (partial), E9 (partial), E14 (partial), E20 (partial) |
| **M1** | A trustworthy agent | The full local toolbelt works headlessly; every writer and shell call passes the permission gate; shell runs inside a real OS jail or is refused; `deny` rules cannot be bypassed in any posture; refusals carry typed codes and correct exit statuses. | E5, E6, E7, E14, E20 |
| **M2** | The TUI | `prx` opens a usable terminal app: streaming transcript with settled prefixes, composer, completion, decision cards, todo panel, status line, four themes, resume picker, rebindable keys. A user could work in it for a day. | E9, E10, E11, E12, E13 |
| **M3** | Context and continuity | A long session crosses the compaction trigger without losing a stated constraint or corrupting history; resume/`--continue`/`--copy` are reliable across a kill; `/context` explains the window. | E8, E9 |
| **M4** | Extensibility | A real third-party MCP server's tools are callable through the proxy; a project skill and a custom slash command work; a `PreToolUse` hook can block a call; `@file` references inject context. | E15, E16, E17, E18 |
| **M5** | Ship it | `prx setup` configures a provider from nothing in a fresh home; `prx doctor` explains a broken environment; artifacts exist for four targets with checksums; the parity matrix in Appendix A is fully accounted for. | E19, E20, E21, E22 |

---

## B2. Epic index

| Epic | Title | M | Tickets | Depends on |
| --- | --- | --- | --- | --- |
| E1 | Project foundation, conventions and CI | M0 | 8 | — |
| E2 | Configuration, paths and identity | M0 | 7 | E1 |
| E3 | Provider layer: registry, wire kinds, streaming | M0 | 10 | E1, E2 |
| E4 | Turn loop, events and the controller | M0 | 9 | E1, E2, E3 |
| E5 | Tool framework and the local toolbelt | M0–M1 | 14 | E2, E4 |
| E6 | Permission engine | M1 | 8 | E2, E5 |
| E7 | Sandbox and workspace trust | M1 | 7 | E2, E6 |
| E8 | Context management and compaction | M3 | 7 | E3, E4, E9 |
| E9 | Sessions, storage and continuity | M0–M3 | 9 | E1, E2 |
| E10 | TUI shell, transcript and rendering | M2 | 8 | E4, E9, E13-T4 |
| E11 | TUI interaction surfaces | M2 | 10 | E10, E12 |
| E12 | Theming, keymap and i18n | M2 | 6 | E10 |
| E13 | Code, diff and markdown rendering | M2 | 5 | E10 |
| E14 | Headless `run`/`-p` and automation contract | M0–M1 | 7 | E4, E5, E6 |
| E15 | MCP client | M4 | 8 | E4, E5, E6 |
| E16 | Skills and custom slash commands | M4 | 7 | E4, E5, E11-T10 |
| E17 | Hooks | M4 | 5 | E4, E6 |
| E18 | Instructions and `@`-references | M4 | 4 | E2, E4 |
| E19 | CLI surface: routing, setup, doctor, completion | M5 | 6 | all M0–M4 |
| E20 | Quality: test harness and end-to-end suite | M0–M5 | 7 | grows with every epic |
| E21 | Documentation and onboarding | M5 | 4 | all |
| E22 | Release and distribution | M5 | 4 | all |

**Critical path:** E1 → E2 → E3 → E4 → E5 → E6 → E7 → E9 → E10 → E11 (the longest chain; every milestone on it is on the path to a usable TUI). E8 and E13 branch off E4/E9. E15–E18 branch off E5/E6 and can proceed in parallel with M2 once M1 closes.

---

## B3. Epic detail

### E1 · Project foundation, conventions and CI — M0

**Goal:** the repository is buildable, testable, lintable and cross-compilable before any feature lands, and the architecture rules are enforced by a test rather than by review.

- **E1-T1 · Module, layout and entry point** — `size:S` · deps: — · labels: `type:chore`, `area:core`, `milestone:m0`, `good-first-issue`
  - AC: `go.mod` declares the module with a pinned toolchain; the full `internal/…` layout from TDD §6.2 exists with package doc comments.
  - AC: `cmd/prx/main.go` calls `cli.Run`; `make build` produces a runnable `bin/prx` that prints its version.
- **E1-T2 · Makefile: build, cross, test, vet, fmt, lint** — `size:S` · deps: `E1-T1` · labels: `type:chore`, `area:core`, `milestone:m0`, `good-first-issue`
  - AC: `make cross` produces `dist/` for `darwin|linux × amd64|arm64` with `CGO_ENABLED=0 -trimpath`.
- **E1-T3 · Architecture test enforcing the import graph** — `size:S` · deps: `E1-T1` · labels: `type:test`, `area:core`, `milestone:m0`
  - AC: `internal/arch_test.go` parses imports and fails on any edge violating TDD §6.2 (e.g. `contract` importing a sibling, `termrender` importing `agent`).
- **E1-T4 · Dependency policy and review checklist** — `size:S` · deps: `E1-T1` · labels: `type:chore`, `area:core`, `milestone:m0`
  - AC: `docs/DEPENDENCIES.md` lists the approved set with a one-line justification each and requires pure-Go + `CGO_ENABLED=0` safety.
  - AC: `golang.org/x/sync` (errgroup, §E4-T5) and `go.uber.org/goleak` (§E20-T7) are recorded there with their reasons, since the practice baseline now requires both.
  - AC: CI fails if a build with `CGO_ENABLED=0` breaks.
- **E1-T5 · Lint config, formatters and the practice baseline** — `size:M` · deps: `E1-T1` · labels: `type:chore`, `area:core`, `milestone:m0`
  - AC: `.golangci.yml` is pinned to `golangci-lint` v2.14.0 (schema `"2"`), curated rather than a preset, and every entry cites the rule it enforces; `golangci-lint config verify` and `golangci-lint run` both pass.
  - AC: `docs/GO-STANDARDS.md` exists with the two-tier map (machine-decided vs audited), the audit protocol, and a §7 entry for every sanctioned divergence from a rule; `go-best-practices.md` is committed and referenced from it.
  - AC: the path-scoped `forbidigo` allow-list is proven to fire by a test — an exclusion that silently never matches is worse than no exclusion.
- **E1-T6 · Continuous integration workflow** — `size:M` · deps: `E1-T2`, `E1-T5` · labels: `type:chore`, `area:quality`, `milestone:m0`
  - AC: a PR runs `golangci-lint config verify` + `golangci-lint run`, `go vet`, a `gofmt`/`goimports` check, `go test -race ./...` (unit tier) on macOS and Linux, and the cross-compile job.
  - AC: the `integration` and `e2e` tiers run with `-race` on a scheduled job rather than on every commit (rule [#83]).
  - AC: the standards test fails if a linter pinned in `.golangci.yml` no longer exists in the running `golangci-lint`.
- **E1-T7 · Commit conventions, PR template, hooks** — `size:S` · deps: `E1-T1` · labels: `type:chore`, `area:quality`, `milestone:m0`, `good-first-issue`
  - AC: PR template asks for the ticket id, the ACs met, and whether a new dependency was added.
- **E1-T8 · Identity single-owner test** — `size:S` · deps: `E2-T2` · labels: `type:test`, `area:config`, `milestone:m0`
  - AC: a test fails if the binary name, home directory name or project config filename appears outside `internal/config/identity.go` and the docs.

### E2 · Configuration, paths and identity — M0

**Goal:** configuration is loaded, resolved, validated and *safe* (a cloned repository cannot widen its own sandbox), with paths and identity in one place.

- **E2-T1 · Config schema types, defaults and TOML load/save** — `size:M` · deps: `E1-T1` · labels: `type:feature`, `area:config`, `milestone:m0`
  - AC: every field in TDD §6.11 has a typed field, a documented default and a round-trip test.
  - AC: an unknown key loads with a warning; a malformed file never gets rewritten.
- **E2-T2 · Paths, roots and identity constants** — `size:M` · deps: `E2-T1` · labels: `type:feature`, `area:config`, `milestone:m0`
  - AC: `~/.prx` (or `PRX_HOME`), the project config path, and every subdirectory are resolved by one table; a test asserts the documented layout.
- **E2-T3 · Resolution order and merge** — `size:M` · deps: `E2-T1` · labels: `type:feature`, `area:config`, `milestone:m0`
  - AC: `flags > ./prx.toml > ~/.prx/config.toml > defaults` with a table-driven test per section.
- **E2-T4 · User-global-only keys** — `size:S` · deps: `E2-T3` · labels: `type:feature`, `area:config`, `risk:security`, `milestone:m0`
  - AC: a project file cannot set credentials paths, `[ui].commandmode` or the equivalent global-only sections; attempted values are held and reported, never applied.
- **E2-T5 · Secret handling: `.env`, `api_key_env`, child-env scrubbing** — `size:M` · deps: `E2-T2` · labels: `type:feature`, `area:config`, `risk:security`, `milestone:m0`
  - AC: provider keys live only in `~/.prx/.env` at `0600`; the file is never printed; a test asserts a tool child process cannot see the variable.
  - AC: a `${VAR}` in a project `.env` expands for MCP/plugin settings but can never supply a provider key.
- **E2-T6 · Narrow-only project keys** — `size:M` · deps: `E2-T3`, `E2-T4` · labels: `type:feature`, `area:config`, `risk:security`, `milestone:m0`
  - AC: a project may add `[permissions] deny`, add `forbid_read`, or restrict roots; a project that tries to widen them is ignored with an `IgnoredProjectSettings` notice listing the keys.
  - AC: table-driven tests for each widen attempt (allow rules, network on, bash off, write roots, tool env).
- **E2-T7 · Config warnings, quarantine and repair** — `size:M` · deps: `E2-T1`, `E2-T3` · labels: `type:feature`, `area:config`, `milestone:m0`
  - AC: `LoadWarnings()` is a first-class value surfaced by the TUI, `run`'s stderr and `prx doctor`.
  - AC: an unparseable config is quarantined with a `.bak` copy and the last-known-good is used, with one notice naming both paths.

### E3 · Provider layer — M0

**Goal:** any OpenAI-compatible, Anthropic-compatible or Responses endpoint is a configuration entry, not a code change; streaming is robust against real-world network behaviour.

- **E3-T1 · `contract/provider` types** — `size:M` · deps: `E1-T1` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: `Provider`, `Request`, `Message`, `ToolCall`, `ToolSchema`, `Chunk`, `ChunkKind`, `Usage` per TDD §6.3 with unit tests on construction and JSON round-trips.
- **E3-T2 · Provider registry and factory contract** — `size:S` · deps: `E3-T1` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: `Register`/`New`/`Kinds`; a duplicate kind returns an error and never panics (rule [#48]); **no `init()` and no blank-import side effects** — a test asserts `internal/assembly` is the only caller of `Register` (rules [#3], TDD §6.2).
- **E3-T3 · `httpclient`: retry, backoff, typed errors** — `size:M` · deps: `E1-T1` · labels: `type:feature`, `area:provider`, `risk:perf`, `milestone:m0`
  - AC: bounded exponential backoff with jitter; `Retry-After` honoured up to a ceiling; retryable statuses limited to 408/429/5xx; errors carry status + retryability so the loop can decide.
  - AC: proxy settings honoured; a test proves no retry on 4xx auth errors.
- **E3-T4 · SSE reader, idle watchdog, interrupted streams** — `size:M` · deps: `E3-T3` · labels: `type:feature`, `area:provider`, `risk:perf`, `milestone:m0`
  - AC: a silent stream beyond `idle_timeout_seconds` (default 300) is read as dropped and retried at most once; a mid-body cut surfaces as a typed `StreamInterruptedError`.
- **E3-T5 · `kind = "openai"` (chat completions)** — `size:L` · deps: `E3-T4`, `E3-T8` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: streams text, reasoning and tool-call deltas; tool-call fragments are accumulated **by index** and only complete calls are emitted.
  - AC: recorded-fixture tests cover text-only, tool-call, reasoning and error responses.
- **E3-T6 · `kind = "anthropic"` (Messages API)** — `size:L` · deps: `E3-T4`, `E3-T8` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: `base_url` root resolution (a trailing `/v1` is stripped and re-derived), `anthropic-version` header, and cache-control breakpoints where the vendor supports them.
- **E3-T7 · `kind = "responses"` (Responses API)** — `size:L` · deps: `E3-T4`, `E3-T8` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: stateless mode works; a `request_url` override is honoured and an override that merely repeats `base_url` is discarded.
- **E3-T8 · Reasoning effort and vendor quirks** — `size:M` · deps: `E3-T1` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: `--effort` and per-model `supported_efforts`/`default_effort` resolve to the correct wire field per vendor; an unsupported effort is reported, not silently dropped.
- **E3-T9 · Usage and cache accounting** — `size:M` · deps: `E3-T5`, `E3-T6` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: cache hit/miss/write tokens, reasoning tokens and `finish_reason` are captured per request and reported per turn.
  - AC: a `finish_reason=length` truncation is classified and surfaced, and a per-turn output ceiling is clipped at send time against the remaining window without changing the compaction trigger.
- **E3-T10 · Model resolution, windows, output caps, cost** — `size:M` · deps: `E2-T3`, `E3-T9` · labels: `type:feature`, `area:provider`, `milestone:m0`
  - AC: a model reference resolves as `provider/model`, a provider name (→ its default), or a bare model name (→ the first provider listing it), with an error naming the candidates on ambiguity.
  - AC: `context_window` and `max_output_tokens` resolve via provider → per-model override → known capability → documented default; per-model prices feed a cost estimate.

### E4 · Turn loop, events and the controller — M0

**Goal:** one turn loop, one event vocabulary, two frontends. This epic is where the "model proposes, host decides" rule becomes code.

- **E4-T1 · Event vocabulary and `Sink` interface** — `size:M` · deps: `E3-T1` · labels: `type:feature`, `area:agent`, `milestone:m0`
  - AC: every event in TDD §6.4 exists as a typed struct with a doc comment; notices and refusals carry a stable `code`.
  - AC: a test asserts `Sink.Emit` is called on a non-blocking contract (a slow sink drops with a counted notice rather than stalling the loop); the sink's capacity is a named constant with its size and policy in a comment (one of the sanctioned buffers, rule [#67]).
- **E4-T2 · Controller and `Runner`** — `size:M` · deps: `E4-T1`, `E9-T1` · labels: `type:feature`, `area:agent`, `milestone:m0`
  - AC: one `Controller` exposing `Run(ctx, input)` plus the port interfaces a frontend needs (turn control, approvals, session history, capabilities); both frontends drive only this.
- **E4-T3 · Turn begin: freeze policy, append host-authored blocks** — `size:M` · deps: `E4-T2` · labels: `type:feature`, `area:agent`, `milestone:m0`
  - AC: the model, effort and preset are frozen at turn start; the execution-policy block is host-authored, marked as such, and appended to the user turn — never to the system prompt.
  - AC: a mid-turn preset change cannot alter the current turn (test).
- **E4-T4 · Model round: request build, stream fold, replay** — `size:L` · deps: `E4-T3`, `E3-T4` · labels: `type:feature`, `area:agent`, `milestone:m0`
  - AC: the request is built once and replayed byte-identically on retry — no synthetic messages are inserted between attempts (test).
  - AC: text/reasoning deltas are emitted as they arrive; only complete tool calls are collected.
- **E4-T5 · Tool batch: barriers, parallel segments, serial writers** — `size:L` · deps: `E4-T4`, `E5-T2` · labels: `type:feature`, `area:agent`, `risk:perf`, `milestone:m0`
  - AC: the batch is cut at the first decision barrier; remaining calls split into **contiguous** parallelisable segments (read-only and not sequential) run with a cap of 8; writers, unknown names and sequential tools run serially in provider order.
  - AC: only one `complete_step` is honoured per round.
  - AC: the parallel run uses `golang.org/x/sync/errgroup` and places results by index into a pre-sized slice; `go test -race` over a batch where every call is parallelisable catches a shared-append regression (rules [#69], [#73]).
- **E4-T6 · Interruption and recovery record** — `size:M` · deps: `E4-T4` · labels: `type:feature`, `area:agent`, `risk:data-loss`, `milestone:m0`
  - AC: cancelling mid-turn stops the in-flight request, marks the turn interrupted, and folds a bounded recovery record into the *next* user turn.
- **E4-T7 · Readiness check** — `size:M` · deps: `E4-T4`, `E5-T13` · labels: `type:feature`, `area:agent`, `milestone:m1`
  - AC: baseline checks run once per turn; declared verification that did not run after the last write yields an unmet-readiness result carrying the attempts and what was missing.
- **E4-T8 · Completion report and verdict** — `size:M` · deps: `E4-T6`, `E4-T7` · labels: `type:feature`, `area:agent`, `milestone:m1`
  - AC: every turn ends with a host-built report; the verdict is derived from host evidence only, and the host never raises a status it cannot prove.
- **E4-T9 · Progress watch and preset plumbing** — `size:M` · deps: `E4-T4` · labels: `type:feature`, `area:agent`, `milestone:m1`
  - AC: idle tool rounds and repeated input-token multiples are counted; the watch **reports** by default and pauses only when configured to.
  - AC: `--preset balanced|delivery` changes host policy (readiness strictness) without changing the provider-visible tool list (test).

### E5 · Tool framework and the local toolbelt — M0–M1

**Goal:** a tool is a small interface, optional behaviour is discovered by assertion, output is bounded once, and the always-on surface stays small enough to reason about.

- **E5-T1 · `Tool` interface, registry, canonical schema cache** — `size:M` · deps: `E2-T1` · labels: `type:feature`, `area:tools`, `milestone:m0`
  - AC: `Add` canonicalises the schema once and caches it; `Builtins()` lists the compile-time set; the agent sees only the registry.
- **E5-T2 · Optional capability interfaces** — `size:S` · deps: `E5-T1` · labels: `type:feature`, `area:tools`, `milestone:m0`
  - AC: `ReadOnly`, `Sequential`, `PlanSafe`, `SnipHinter`, `Paged`, `Preflighter`, `DecisionBarrier`, `PathWriter` are asserted, never name-matched; a test asserts a tool adding one changes scheduling without touching the loop.
- **E5-T3 · Output bounding, `RawContent`, spill** — `size:M` · deps: `E5-T1` · labels: `type:feature`, `area:tools`, `risk:perf`, `milestone:m0`
  - AC: results are bounded once on first model entry at ≤32 KiB visible; the full body is kept only when it differs; oversize results spill to a session file and are paged.
  - AC: a test asserts maintenance never rewrites a stored tool body.
- **E5-T4 · Read-only tools: `read_file`, `ls`, `glob`, `grep`** — `size:L` · deps: `E5-T3` · labels: `type:feature`, `area:tools`, `milestone:m0`, `good-first-issue`
  - AC: `read_file` is line-numbered with offset/limit and defaults to 2000 lines; `glob`/`grep` enforce result and timeout ceilings; `ls` skips VCS and dependency noise.
- **E5-T5 · `write_file` and write-root resolution** — `size:M` · deps: `E7-T1` · labels: `type:feature`, `area:tools`, `risk:security`, `milestone:m1`
  - AC: creation of parent directories; refusal outside write roots with a typed error; `protect_changed_files` refuses a whole-file write over a change made since the agent last read or wrote the file.
- **E5-T6 · `edit_file` and `multi_edit`** — `size:M` · deps: `E5-T5` · labels: `type:feature`, `area:tools`, `milestone:m1`
  - AC: `edit_file` requires a unique `old_string` and names the ambiguity count when it is not unique; `multi_edit` applies atomically — all edits succeed or the file is untouched.
- **E5-T7 · `move_file` and `delete_range`** — `size:M` · deps: `E5-T5` · labels: `type:feature`, `area:tools`, `milestone:m1`
  - AC: `move_file` refuses an existing destination and checks **both** ends against write roots; `delete_range` requires unique anchors and returns a unified diff.
- **E5-T8 · `bash`** — `size:L` · deps: `E7-T3`, `E7-T4`, `E6-T5` · labels: `type:feature`, `area:tools`, `risk:security`, `milestone:m1`
  - AC: runs inside the sandbox spec; foreground timeout enforced host-side; a background invocation returns a job id; combined stdout/stderr; a write outside the roots is denied by the sandbox and reported as a typed failure.
- **E5-T9 · Background jobs: `bash_output`, `kill_shell`, `wait`** — `size:M` · deps: `E5-T8` · labels: `type:feature`, `area:tools`, `milestone:m1`
  - AC: output is readable incrementally without blocking; a killed job reports its status; jobs are cleaned up at session end and their output is bounded.
- **E5-T10 · `web_fetch`** — `size:S` · deps: `E3-T3` · labels: `type:feature`, `area:tools`, `risk:security`, `milestone:m1`
  - AC: HTML is reduced to text; private/link-local addresses are blocked with loopback allowed; response size is capped.
- **E5-T11 · `ask` and `await_user`** — `size:M` · deps: `E4-T1` · labels: `type:feature`, `area:tools`, `milestone:m1`
  - AC: `ask` takes 1–4 questions with options, a reason and multi-select; both tools are decision barriers that stop the batch.
  - AC: a headless run with no approver resolves `ask` per posture (§E6-T7) rather than hanging.
- **E5-T12 · `todo_write` and its invariants** — `size:M` · deps: `E4-T2` · labels: `type:feature`, `area:tools`, `milestone:m1`
  - AC: at most two levels; exactly one `in_progress`; sub-steps complete in order; a level-1 item with no phase above it is rejected.
  - AC: rejects removing or regressing the in-progress item, breaking the completed prefix, and newly completing an item with no matching `complete_step` receipt; re-sending the current list is acknowledged as unchanged.
- **E5-T13 · `complete_step` and host adjudication** — `size:L` · deps: `E5-T12` · labels: `type:feature`, `area:tools`, `risk:data-loss`, `milestone:m1`
  - AC: at least one evidence item is required (`verification`/`review`/`diff`/`files`/`manual`); the host verifies cited commands against recorded successful runs and cited paths against write/read receipts; `manual` cannot stand alone once receipts exist.
  - AC: the host advances the list itself and emits the resulting todo state; the tool is refused in plan mode.
- **E5-T14 · Tool-contract documentation generation and drift test** — `size:S` · deps: `E5-T3` · labels: `type:test`, `area:tools`, `milestone:m1`
  - AC: `docs/tool-contract.md` is generated from the registry and a test fails on drift; the doc states the lean provider-visible surface versus what is reachable through the capability proxy.

### E6 · Permission engine — M1

**Goal:** per-call, model-independent, host-side gating where `deny` always wins and the user is asked once per *scope*, not once per *call*.

- **E6-T1 · Rule grammar: parse, format, match** — `size:M` · deps: `E2-T1` · labels: `type:feature`, `area:perm`, `milestone:m1`
  - AC: `Tool`, `Tool(specifier)` and legacy `Tool=literal` parse and round-trip through formatting; `*` crosses `/` in a glob; malformed rules are reported with a line reference.
- **E6-T2 · Subject extraction** — `size:M` · deps: `E6-T1` · labels: `type:feature`, `area:perm`, `milestone:m1`
  - AC: subjects come from a fixed key set in the tool args (`command`, `path`/`file_path`, `pattern`, `source_path`/`destination_path`, `planId`); `move_file` yields both endpoints; a rule whose subject the args do not expose only matches in bare form.
- **E6-T3 · `Policy.Decide` with full precedence** — `size:M` · deps: `E6-T2` · labels: `type:feature`, `area:perm`, `risk:security`, `milestone:m1`
  - AC: precedence is `deny → session grant → ask → allow → read-allowed → human-required → posture fallback`, pure and I/O-free.
  - AC: table-driven tests prove `deny` wins over a broad `allow` **and** over bypass, and that `ask` carves a subset out of a broad allow.
- **E6-T4 · `Gate`, `Approver` and grant scopes** — `size:M` · deps: `E6-T3` · labels: `type:feature`, `area:perm`, `milestone:m1`
  - AC: an approval offers allow-once, allow-for-session (scoped) and always-allow (persisted) plus deny, and echoes the scope that will be granted.
- **E6-T5 · Bash-specific evaluation** — `size:L` · deps: `E6-T2` · labels: `type:feature`, `area:perm`, `risk:security`, `milestone:m1`
  - AC: commands are parsed and evaluated per segment; `:*` prefix rules reject a command that introduces shell operators later in the line; wrapper commands are peeled before matching.
  - AC: dynamic forms (expansion, assignment, heredoc, redirect, glob) cannot inherit a bare/prefix/glob allow; nested/indirect forms (substitution, dynamic command name, `eval`, `source`, `sh -c`) require a human in Ask/Auto and are covered only by an identical exact grant or bypass; `allow_dynamic_bash` relaxes the fallback class only.
  - AC: recursive deletion of the home directory, a filesystem root, a workspace ancestor or a path outside the roots is hard-refused in every posture with typed codes.
- **E6-T6 · Learned-rule shaping and persistence** — `size:M` · deps: `E6-T4` · labels: `type:feature`, `area:perm`, `milestone:m1`
  - AC: a bash grant persists as the safe command prefix when available and an exact literal otherwise; a file-mutation grant persists as `Edit(<path>)` shared by all file-mutating tools; persisted rules are written only to the user config.
- **E6-T7 · Headless resolution and refusal codes** · `size:M` · deps: `E6-T3` · labels: `type:feature`, `area:perm`, `risk:security`, `milestone:m1`
  - AC: with no approver, Ask/manual fails closed for Ask decisions and writer fallback, Auto allows the ordinary writer fallback, bypass resolves ordinary prompts, and deny always blocks.
  - AC: each refusal carries one of `permission.unattended`, `permission.untrusted_folder`, `permission.read_only`, `permission.deny_rule`, `permission.declined`, with a remedy where one exists.
- **E6-T8 · Read-only and plan postures** — `size:M` · deps: `E6-T3`, `E4-T2` · labels: `type:feature`, `area:perm`, `milestone:m1`
  - AC: `read-only` refuses every non-read before any allow rule is consulted; `plan` is a posture that pairs with phase opt-outs (`complete_step` is read-only yet plan-unsafe and must be refused).
  - AC: shell commands proven to read (`git status`, `ls`, `grep`, `git -C <dir> log`) run without a prompt in every mode, decided from the parsed command rather than its wording.

### E7 · Sandbox and workspace trust — M1

**Goal:** enforcement beneath policy. Confinement that is on by default, refuses honestly when it cannot be honoured, and cannot be widened by a project file.

- **E7-T1 · `Sandbox.Spec` and write-root resolution** — `size:M` · deps: `E2-T6` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`
  - AC: roots resolve to absolute symlink-free paths; `..` traversal and a symlink inside a root cannot launder a write out of it (tests for both).
- **E7-T2 · File-writer confinement and `forbid_read`** — `size:M` · deps: `E7-T1` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`
  - AC: every mutating file tool refuses a target outside the roots, feeding the error back to the model; `forbid_read` blocks read/list/search with `${VAR}` expansion supported.
- **E7-T3 · macOS Seatbelt backend** — `size:L` · deps: `E7-T1` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`, `blocked-by:external`
  - AC: a generated SBPL profile denies writes by default and re-allows the roots, denies reads *and* writes under `forbid_read`, and denies network unless enabled; a test proves a write outside the roots fails and a write inside succeeds.
- **E7-T4 · Linux bubblewrap backend** — `size:L` · deps: `E7-T1` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`, `blocked-by:external`
  - AC: read-only root bind with tmpfs/bind write roots; network unshared unless enabled; a test proves the same two cases as E7-T3.
- **E7-T5 · No-backend policy and platform scope** — `size:S` · deps: `E7-T3`, `E7-T4` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`
  - AC: with `bash = "enforce"` and no available backend, shell **refuses** to run with a typed error naming the backend to install; `bash = "off"` is an explicit, documented opt-out; `prx doctor` reports the effective mode.
- **E7-T6 · Workspace trust store, prompt and `prx trust`** — `size:M` · deps: `E2-T2` · labels: `type:feature`, `area:sandbox`, `risk:security`, `milestone:m1`
  - AC: trust is per resolved path, stored in `~/.prx/trust.json` at `0600`, never written into the project; `prx trust` / `--revoke` manage it; the prompt is a decision card in the TUI and is never shown for a home directory or a filesystem root.
- **E7-T7 · Default posture selection and extra roots** — `size:M` · deps: `E7-T5`, `E7-T6`, `E6-T7` · labels: `type:feature`, `area:sandbox`, `milestone:m1`
  - AC: with no mode named, a session opens in `auto` only when the sandbox confines shell writes **and** the folder is trusted; otherwise `ask`.
  - AC: `--add-dir` extends write boundaries for the session only (never persisted), resolves relative to the workspace root, and requires existing directories.
  - AC: outside-sandbox toolchain temp/cache roots are added explicitly and documented; write-root identity is pinned for the session, and a changed real path fails closed.

### E8 · Context management and compaction — M3

**Goal:** long sessions survive; a constraint the user stated ten turns ago survives with them; nothing is rewritten below the trigger.

- **E8-T1 · Trigger and capacity** — `size:M` · deps: `E3-T10`, `E4-T4` · labels: `type:feature`, `area:context`, `milestone:m3`
  - AC: `triggerTokens = floor(context_window × compact_ratio)` with `compact_ratio` default 0.85 and strictly bounded; `context_window = 0` disables compaction.
  - AC: below the trigger a test asserts **no** write happens: no summary, no projection, no sidecar, no version bump.
- **E8-T2 · The compaction transaction** — `size:L` · deps: `E8-T1` · labels: `type:feature`, `area:context`, `risk:data-loss`, `milestone:m3`
  - AC: candidate = pinned prefix + one structured digest (Standing facts, Goal, Decisions, Files, Commands, Errors, Pending) + recent verbatim tail; acceptance requires ≤50 % of window, strictly smaller than the source, and below the trigger; candidates are never padded.
  - AC: the pinned prefix is the system prompt plus the first user turn when it is small enough (≤1500 tokens and ≤15 % of the window); the tail budget is `clamp(window × 10 %, 32 K, 96 K)`.
- **E8-T3 · Keep policy and user-turn retention** — `size:L` · deps: `E8-T2` · labels: `type:feature`, `area:context`, `risk:data-loss`, `milestone:m3`
  - AC: always retained verbatim: system prompt, first user turn when small, every user turn that fits the retention budget, the recent tail, error-carrying tool results (classified from the **host's execution record**, not from log text), the active turn's opening user turn, and `[[keep]]`-marked messages.
  - AC: retention is bounded at 1500 tokens per turn and `min(8192, window × 5 %)` in total, oldest-first; a drop is counted (`user_kept`/`user_dropped`) and a committed checkpoint that folded a user turn emits a warning naming `[[keep]]`.
- **E8-T4 · Projection versus canonical transcript** — `size:M` · deps: `E8-T2` · labels: `type:feature`, `area:context`, `risk:data-loss`, `milestone:m3`
  - AC: the canonical transcript is never rewritten; compaction writes a projection only; each fold re-derives from the canonical source so digests do not chain.
  - AC: `ModelMessages` strips `RawContent` and host bookkeeping so serialisation and cache hashes never include them, while the stored projection keeps them; a test asserts a second fold can still classify a protected failure.
- **E8-T5 · Failure receipts and manual retry** — `size:M` · deps: `E8-T2`, `E9-T4` · labels: `type:feature`, `area:context`, `milestone:m3`
  - AC: a generation that failed records a typed blocked receipt and does not attempt a second automatic summary; a summary truncated at the output cap retries once with a larger cap; manual `compress` can retry.
- **E8-T6 · `compress` and `recall`** — `size:M` · deps: `E8-T4` · labels: `type:feature`, `area:tools`, `milestone:m3`
  - AC: `compress` requires an explicit user request and is a barrier; `recall` reads by `#n` position or searches the covered region under a per-generation budget (`window × 10 %`, floor 2000) and is refused whole when the budget is exceeded.
- **E8-T7 · `context_budget` and the `/context` breakdown** — `size:M` · deps: `E8-T1` · labels: `type:feature`, `area:context`, `milestone:m3`
  - AC: remaining room is available to both the model (tool) and the user (`/context`), broken down by system prompt, instructions, tool schemas, memory, verbatim history, digest and headroom.

### E9 · Sessions, storage and continuity — M0–M3

**Goal:** committed history is never lost, a fold is an explicit event, and the picker is informative before it loads anything.

- **E9-T1 · Transcript model** — `size:M` · deps: `E2-T1` · labels: `type:feature`, `area:state`, `milestone:m0`
  - AC: `Message` per TDD §6.3 including `RawContent` and `Hidden`; the in-memory transcript is append-only and its fold to provider messages is a pure function.
- **E9-T2 · Event log writer** — `size:M` · deps: `E9-T1` · labels: `type:feature`, `area:state`, `risk:data-loss`, `milestone:m0`
  - AC: `append` and `replace` records with `revision`, `base_revision`, `message_index`, `content_digest`; `append` requires `message_index == current length` (the chain check); every record is fsynced; files are `0600`.
  - AC: the legacy-style canonical `.jsonl` is written alongside and the reader prefers the event log.
- **E9-T3 · Tolerant replay, salvage and log folding** — `size:L` · deps: `E9-T2` · labels: `type:feature`, `area:state`, `risk:data-loss`, `milestone:m0`
  - AC: replay stops at the first unparseable or non-chaining record, returns the valid prefix, truncates to the last good boundary and moves the discarded bytes to `.damaged` with a notice.
  - AC: an unknown schema version or record type is a **hard** error (a newer writer owns the file) and never guessed at; size/count are preflighted before materialising.
  - AC: the log folds to a single `replace` past a size or growth threshold, atomically via temp + rename.
- **E9-T4 · Index sidecar** — `size:M` · deps: `E9-T2` · labels: `type:feature`, `area:state`, `milestone:m0`
  - AC: the index holds turn count, title, preview, model, timestamps and digests; it is treated as a cache and repaired — never trusted over the log — when its digest disagrees.
- **E9-T5 · Lifecycle: new, resume, continue, copy** — `size:M` · deps: `E9-T4` · labels: `type:feature`, `area:state`, `milestone:m0`
  - AC: `--continue` resumes the newest session or starts a fresh one with a notice; `--resume QUERY` accepts an id, a path, or a unique title/preview substring and reports missing and ambiguous matches distinguishably; `--copy` leaves the original untouched.
- **E9-T6 · Picker data and preview metadata** — `size:M` · deps: `E9-T4` · labels: `type:feature`, `area:state`, `milestone:m2`
  - AC: listing reads only the index and **never decodes a transcript** (asserted by a test); each row shows title, model, turn count, age and a first-prompt excerpt; an unindexed session shows "indexing" and is repaired in the background (U10).
- **E9-T7 · Single-writer lease** — `size:M` · deps: `E9-T2` · labels: `type:feature`, `area:state`, `risk:data-loss`, `milestone:m2`
  - AC: opening a session for writing takes a lease; a second writer gets a typed error naming the holder and is offered `--copy`; a stale lease from a dead process is reclaimable with a notice.
- **E9-T8 · In-flight turn record and crash repair** — `size:M` · deps: `E9-T4` · labels: `type:feature`, `area:state`, `risk:data-loss`, `milestone:m2`
  - AC: a turn in flight records its start index, revision and digest plus its commit digest; on resume an in-flight turn is truncated when the commit digest is absent and kept when it is present — never invented.
- **E9-T9 · Session directory layout, spill and permissions** — `size:S` · deps: `E9-T2` · labels: `type:chore`, `area:state`, `milestone:m0`
  - AC: the documented layout is created with the documented modes (transcripts `0600`, index `0644`); spilled tool output lives under the session's `outputs/` and is bounded.

### E10 · TUI shell, transcript and rendering — M2

**Goal:** a terminal app that stays responsive while a model streams and never prints something it might want to retract.

- **E10-T1 · Program skeleton, options and terminal setup/restore** — `size:M` · deps: `E4-T2` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: `Run(ctx, Options)` builds the program with inline/fullscreen modes; the terminal is restored on exit, on panic and on interrupt (a test drives setup/restore with a fake terminal).
  - AC: every timer (spinner, flash, edge scroll, status poll) reads the injected clock, so a test advances the fake and drives the timer without sleeping (rules [#86], [#87]).
- **E10-T2 · Event pump: buffered sink, coalescing, drop policy** — `size:M` · deps: `E10-T1`, `E4-T1` · labels: `type:feature`, `area:tui`, `risk:perf`, `milestone:m2`
  - AC: all queued events are drained into ONE update message so a burst costs one render; the sink never blocks the loop; dropping is surfaced as a counted notice rather than silent loss.
- **E10-T3 · Transcript reducer and the settled rule** — `size:L` · deps: `E10-T2` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: `Apply(event)` produces typed rows per TDD §6.12.2; deltas fold into a growing row; committed messages win over deltas; tool dispatch/progress/result merge by call id and an empty field never erases a previous value.
  - AC: a row is settled only up to the last blank line **outside a fenced block**; a test drives a fenced streaming answer and asserts the settled boundary.
- **E10-T4 · Viewport, pinned bottom region, scroll and follow** — `size:M` · deps: `E10-T3` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: the bottom region (composer + status) is pinned; following resumes on new output only when the user was already at the bottom; page/home/end navigation works while streaming.
- **E10-T5 · Overlay ownership contract** — `size:S` · deps: `E10-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: an overlay declares `OwnsKeys` and `ComposerVisible`; the layout derives bottom height from the declaration, so an inactive composer cannot render under a modal (U2).
- **E10-T6 · Inline mode as a sink** — `size:L` · deps: `E10-T3` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: inline prints settled rows to the terminal's scrollback and bounds its live region to `min(height − chrome − 1, height/2)`; the same reducer drives both modes and no row kind behaves differently except in where it is printed (U3).
  - AC: `--inline` is available on the TUI verb and is the default when stdout is not a TTY (Q3).
- **E10-T7 · Expanded reasoning and turn folding** — `size:M` · deps: `E10-T3` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: `/verbose` toggles expanded reasoning; a completed turn can fold its tool work into a summary line; an unexpanded reasoning row is one line with a token count.
- **E10-T8 · Resize handling and render cache** — `size:M` · deps: `E10-T3` · labels: `type:feature`, `area:tui`, `risk:perf`, `milestone:m2`
  - AC: on resize, rows re-render at the new width instead of keeping stale wrapping; the render cache is keyed by width and fold state and invalidates only affected rows.

### E11 · TUI interaction surfaces — M2

**Goal:** every interruption looks and behaves like every other interruption, and every key is discoverable.

- **E11-T1 · Composer** — `size:M` · deps: `E10-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: multi-line with `Enter` send and `Shift+Enter`/`Alt+Enter`/`Ctrl+J` newline; dynamic height capped at 8 rows; a themed cursor per `[ui] cursor_shape`; paste of multi-line text is folded into an editable token.
- **E11-T2 · Completion menus for `/` and `@`** — `size:M` · deps: `E11-T1` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: one menu implementation; `Tab`/`Enter` accept; `@` descends exactly one directory level per keystroke using a single directory read (never a recursive walk) and lists MCP resources beside files; the menu changes height only on discrete actions, never per streamed token.
- **E11-T3 · Decision card component and posture keys** — `size:L` · deps: `E10-T5`, `E6-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: one component renders approval, `ask`, workspace trust, sandbox-escape and error-recovery prompts with consistent verbs (`y`/`a`/`p`/`n` plus number keys), a preview pane, and the stable refusal code that tests assert on (U5).
  - AC: `Shift+Tab` cycles read-only → ask → auto → YOLO → plan and `Ctrl+Y` toggles YOLO, restoring the previous posture on toggle back.
- **E11-T4 · Pickers** — `size:M` · deps: `E10-T5` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: one searchable picker used by session/model/provider/theme selection; arrow and `Ctrl+P`/`Ctrl+N` move; `j`/`k` navigate while the query is empty and become query text once typing starts; `Esc` cancels without side effects.
- **E11-T5 · Todo panel** — `size:M` · deps: `E10-T3`, `E5-T12` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: an 8-row window centred on the `in_progress` item; two-level indentation; completed items show their sign-off evidence kind; the panel appears only while a list exists.
- **E11-T6 · Status line** — `size:M` · deps: `E10-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: `compact` shows posture and interaction state on the left and model/effort on the right; `full` adds cache hit rate, context use, compaction headroom, git identity and background job count; `off` hides it; segments are configurable (U4).
  - AC: narrow terminals move or compact whole groups rather than cutting a label in half.
- **E11-T7 · Image paste and attachment tokens** — `size:M` · deps: `E11-T1` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: a clipboard image inserts an editable `[image #N]` token and the status line reports progress until it is ready; `/paste-image` does the same; the image is attached only if the selected model accepts images.
- **E11-T8 · Mouse, selection and clipboard** — `size:M` · deps: `E10-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: mouse is off by default when the session is remote; `/mouse` toggles; wheel scroll, click-to-fold, drag-select and a draggable scrollbar work; copying a selection uses the platform clipboard (with an OSC 52 fallback over SSH) and reports success only after the write completes.
- **E11-T9 · Notices and flashes** — `size:S` · deps: `E10-T3` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: notices are typed, level-styled and coalesced with an `×N` count; a flash is bounded in time and never occludes the composer.
- **E11-T10 · Slash command registry, dispatch and `/help`** — `size:L` · deps: `E10-T4` · labels: `type:feature`, `area:tui`, `milestone:m2`
  - AC: one registry drives completion, `/help` and dispatch so the displayed list cannot drift from what is accepted; the v1 command set from Appendix A §A4.3 is implemented; MCP prompt commands and project commands appear dynamically.
  - AC: unknown commands produce a suggestion based on edit distance rather than a bare error.

### E12 · Theming, keymap and i18n — M2

**Goal:** it looks deliberate on a truecolor terminal and on a 256-colour one, every key is rebindable, and no user-facing string is hard-coded.

- **E12-T1 · `Palette` with pre-resolved fallbacks** — `size:M` · deps: `E10-T1` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: named colours include accents, muted/faint/subtle, semantic levels and diff backgrounds; every colour carries a hand-chosen 256-colour fallback so a downsampling terminal does not collapse red/green into one tint.
- **E12-T2 · Theme selection, persistence and auto detection** — `size:M` · deps: `E12-T1` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: `/theme [auto|dark|light|<style>]` switches live and persists; `PRX_THEME`/`PRX_THEME_STYLE` override a single run; `auto` probes the terminal background when we own it, then falls back to `COLORFGBG`, then to dark.
  - AC: at least two dark and two light accent styles are distinguishable in a 256-colour terminal.
- **E12-T3 · Keymap registry** — `size:L` · deps: `E10-T4` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: every action has a default binding, a context and a help string; config overrides apply; an unknown action or a conflicting binding is a startup error naming both.
  - AC: `/keys` renders the live registry and `/help` renders it grouped by context, both from the same source as dispatch (U1).
- **E12-T4 · Cursor, composer and selection theming** — `size:S` · deps: `E12-T1` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: `[ui] cursor_shape` normalises to `bar`/`block`/`underline` with `bar` as the fallback for an unknown value; composer border, cursor, selection and scrollbar all follow the active theme.
- **E12-T5 · i18n catalog and detection** — `size:M` · deps: `E10-T1` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: every user-facing string in `tui`, `termrender`, `run` and `cli` goes through the catalog (enforced by a test that greps for raw literals in view code); language resolves from config, then `$PRX_LANG`, then `$LANG`, then a default.
- **E12-T6 · Output styles and reasoning language** — `size:M` · deps: `E4-T3` · labels: `type:feature`, `area:theme`, `milestone:m2`
  - AC: `/output-style` selects an answer persona that is injected as host-authored context (never into the stable system prompt); `[agent] reasoning_language = auto|en|zh` affects visible reasoning text only and is distinct from UI language.

### E13 · Code, diff and markdown rendering — M2

**Goal:** code and diffs are the content; they must be fast, width-correct and impossible to get subtly wrong.

- **E13-T1 · Markdown renderer** — `size:M` · deps: `E12-T1` · labels: `type:feature`, `area:render`, `milestone:m2`
  - AC: GFM tables, headings, lists, emphasis and fenced code render at the target width; a fenced block without a known language falls back to plain; width correctness is asserted for a CJK case.
- **E13-T2 · Tool cards** — `size:M` · deps: `E12-T1`, `E5-T2` · labels: `type:feature`, `area:render`, `milestone:m2`
  - AC: a card shows verb, primary argument, a category-coloured marker and elapsed time while running; a settled card folds to one line and expands on demand; a refusal is rendered as a refusal with its code, not as an ordinary result.
- **E13-T3 · Diff renderer** — `size:L` · deps: `E13-T1`, `E5-T3` · labels: `type:feature`, `area:render`, `risk:perf`, `milestone:m2`
  - AC: colourised adds/removes with a line-number gutter and hunk headers; the fold is computed **before** syntax highlighting; each `@@` header settles the rows before it so a streaming fence colours hunk by hunk.
  - AC: an optional user-configured external formatter runs off the render path, is memoised by content hash + width, is disabled after a timeout, and only invalidates the blocks that consulted it; any failure falls back to the built-in renderer.
- **E13-T4 · `cells`: width measurement, cutting, wrapping** — `size:L` · deps: `E1-T1` · labels: `type:feature`, `area:render`, `risk:perf`, `milestone:m2`, `good-first-issue`
  - AC: one package owns grapheme-aware measurement **and** the cut/wrap/truncate operations, with a test asserting they agree (a string cut to width `n` measures ≤ `n`).
  - AC: glyphs the console cannot draw are substituted with stand-ins of the same counted width; rows are padded to exactly the content width so the scrollbar column never shifts.
- **E13-T5 · Embedded diff detection for shell results** — `size:S` · deps: `E13-T3` · labels: `type:feature`, `area:render`, `milestone:m2`
  - AC: `[agent] embedded_diff_detection = true` renders a shell result whose whole output is a unified diff (including a `git show` commit header) as a diff; mixed output and `--stat` stay prose; default off.

### E14 · Headless `run`/`-p` and automation contract — M0–M1

**Goal:** the same engine, scriptable, with exit codes a CI can trust.

- **E14-T1 · `run` command: flags and assembly** — `size:M` · deps: `E4-T2`, `E5-T4` · labels: `type:feature`, `area:cli`, `milestone:m0`
  - AC: accepts the task from argv or stdin; flags may appear before or after the verb per TDD §6.14; assembly uses the same builder as the TUI.
- **E14-T2 · Streamed terminal presentation for `run`** — `size:M` · deps: `E14-T1` · labels: `type:feature`, `area:cli`, `milestone:m0`
  - AC: without `-p` or a structured format, output streams to the terminal with the same row semantics as the TUI but no screen control; a clean run writes nothing to stderr except warnings the user must act on.
- **E14-T3 · `-p`/`--print` and prompt sources** — `size:S` · deps: `E14-T1` · labels: `type:feature`, `area:cli`, `milestone:m0`
  - AC: `-p` prints only the final answer; a prompt can come from a positional argument, a file, or stdin; `-p` before the verb reroutes to `run --print` per the reference's placement rules.
- **E14-T4 · Output formats: text / json / stream-json** — `size:M` · deps: `E14-T1`, `E4-T1` · labels: `type:feature`, `area:cli`, `milestone:m1`
  - AC: `stream-json` emits one event object per line (with session, turn and sequence) and ends with a single result object carrying subtype, error flag, duration, turn count, session id, usage, cost, permission denials and permission mode; structured modes never also print a duplicate human-readable error.
- **E14-T5 · Exit codes and refusal reporting** — `size:M` · deps: `E6-T7` · labels: `type:feature`, `area:cli`, `risk:compat`, `milestone:m1`
  - AC: `0` finished (including with refusals or unmet readiness), `1` run failure, `2` usage error, `3` `--fail-on-unverified` unmet, `4` untrusted folder — each covered by an end-to-end test.
  - AC: refused calls are listed with their code and remedy; a refusal never changes the exit code by itself.
- **E14-T6 · `--events-jsonl` redacted sink** — `size:M` · deps: `E4-T1` · labels: `type:feature`, `area:cli`, `risk:security`, `milestone:m1`
  - AC: lifecycle records carry schema version, sequence and kind, and contain **no** prompts, reasoning, tool arguments, tool output or approval text; the final record is a run-done summary; the flag cannot be combined with `--output-format`.
- **E14-T7 · `--metrics`** — `size:S` · deps: `E3-T9` · labels: `type:feature`, `area:cli`, `milestone:m1`
  - AC: writes a machine-readable total (tokens, cache reads/writes, cost, duration, turn and tool-call counts) so a run is comparable without reading its transcript.

### E15 · MCP client — M4

**Goal:** a third-party MCP server's tools are callable, namespaced, permission-gated, and invisible in the prompt until asked for.

- **E15-T1 · Plugin spec and config source** — `size:M` · deps: `E2-T3` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: a `[[plugins]]` entry compiles to a `Spec` with transport, command/args/env or url/headers, plus per-server and global timeouts; `${VAR}` and `${VAR:-default}` expand in command/args/env/url/headers, and `${REASONIX_WORKSPACE_ROOT}`-style workspace variables win over an environment variable of the same name.
- **E15-T2 · Transports: stdio and http** — `size:L` · deps: `E15-T1` · labels: `type:feature`, `area:ext`, `risk:security`, `milestone:m4`
  - AC: stdio spawns a persistent subprocess with one JSON message per line and preserves state across calls; http posts each request and echoes a session id header once seen; cross-origin redirects are refused so credentials cannot leak.
  - AC: lifecycle is `initialize` → `initialized` notification → `tools/list`; a startup timeout and a call timeout are both enforced; the child is terminated on context cancellation and shutdown.
- **E15-T3 · Prompts and resources** — `size:M` · deps: `E15-T2` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: `prompts/list` + `get` surface as `/mcp__<server>__<prompt>`; `resources/list` + `read` surface as `@<server>:<uri>`; `/mcp` shows connected servers with counts and errors.
- **E15-T4 · Tool adaptation, namespacing and read-only hints** — `size:M` · deps: `E15-T2` · labels: `type:feature`, `area:ext`, `risk:security`, `milestone:m4`
  - AC: remote tools are adapted to `Tool` and named `mcp__<server>__<tool>` with spaces normalised and a hash suffix only on collision.
  - AC: `annotations.readOnlyHint: true` maps to `ReadOnly()`, defaults to **false** (a remote tool is opaque), and a destructive hint keeps the tool out of the read-only scheduling path; installing a server is the trust decision, with deny rules and the sandbox still host-controlled.
- **E15-T5 · `use_capability` proxy** — `size:L` · deps: `E15-T4`, `E4-T5` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: the always-on prompt carries one stable schema; `search`/`inspect` discover MCP tools, and `call` re-checks enablement and the live connection identity immediately before dispatch.
  - AC: a proxy call passes the same permission gate as a direct call; the tool is sequential.
- **E15-T6 · Config sources and precedence** — `size:M` · deps: `E15-T1` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: project `.mcp.json` (Claude schema) is read after TOML and merged, with the project `prx.toml` winning on a name clash; provenance is retained so an edit writes back to the file that declared the server.
- **E15-T7 · `prx mcp` subcommands** — `size:M` · deps: `E15-T1` · labels: `type:feature`, `area:cli`, `milestone:m4`
  - AC: `add`, `list`, `remove`, `enable`, `disable`, `retry` work against the effective config; removing a server reveals the next lower-priority declaration; `--json` output is stable.
- **E15-T8 · Example MCP server and end-to-end test** — `size:M` · deps: `E15-T2`, `E20-T2` · labels: `type:test`, `area:ext`, `milestone:m4`
  - AC: `examples/mcp-echo` is a runnable stdio server; an end-to-end test builds it, registers it, calls two tools through the proxy and asserts results and permission behaviour.

### E16 · Skills and custom slash commands — M4

**Goal:** a directory of markdown files extends the agent's behaviour without touching the prompt cache.

- **E16-T1 · Skill discovery, roots and precedence** — `size:M` · deps: `E2-T2` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: roots are searched project → `[skills] paths` → `~/.prx/skills` → `.claude` conventions; precedence is project > custom > global > builtin; a duplicate name resolves to the highest root with a shadowing warning.
- **E16-T2 · Frontmatter, index line and disable rules** — `size:M` · deps: `E16-T1` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: `name`, `description`, `argument-hint`, `allowed-tools`, `read-only`, `invocation`, `disable-model-invocation` and `user-invocable` parse; a malformed file is skipped with a warning, never fatal.
  - AC: only a compact index line per skill enters the prompt so the prefix stays cache-stable; `[skills] disabled_skills` hides a skill from the prompt, the slash surface and the tools.
- **E16-T3 · Inline skill execution** — `size:M` · deps: `E16-T2`, `E5-T14` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: `read_skill` returns the body plus referenced files and a script listing; a skill declaring `read-only` narrows the effective registry for its execution; `runAs: subagent` is refused with a clear "not in this version" error rather than silently degraded.
- **E16-T4 · Custom slash commands and the `slash_command` tool** — `size:M` · deps: `E16-T1` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: `.prx/commands/**/*.md` loads with `description`/`argument-hint` frontmatter and `$ARGUMENTS`/`$1..$N`/`$$` substitution; a subdirectory namespaces the name (`git/commit.md` → `/git:commit`); a project command overrides a global one of the same name.
  - AC: the model can invoke a project command through the `slash_command` tool, and user invocation of the same command renders identically.
- **E16-T5 · `/skills` surface and skill creation flow** — `size:M` · deps: `E16-T2`, `E11-T10` · labels: `type:feature`, `area:tui`, `milestone:m4`
  - AC: `/skills` lists discovered skills with their root and shadowed state; `/skills disable <name>` hides one; a creation flow writes a well-formed `SKILL.md` with required frontmatter.
- **E16-T6 · `.claude/commands` read compatibility** — `size:S` · deps: `E16-T4` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: commands in `<project>/.claude/commands/` are discovered and namespaced so an existing Claude-configured project works unchanged; a name clash resolves in favour of the project's `.prx/commands` with a shadowing notice.
- **E16-T7 · Skill and command diagnostics** — `size:S` · deps: `E16-T5` · labels: `type:feature`, `area:cli`, `milestone:m4`
  - AC: `prx doctor capabilities` reports every skill/command/hook with source path and issues (missing description, shadowed, disabled, unreadable) and a stable issue code; `--live` connects MCP servers for a true view.

### E17 · Hooks — M4

**Goal:** a shell command can observe or block a turn at a small, well-defined set of events.

- **E17-T1 · Hook configuration, scopes and matchers** — `size:M` · deps: `E2-T3` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: hooks load from the project settings file and the global settings file, resolved project → global with array order preserved; a `match` is an anchored regex over tool names where empty or `*` matches all; matchers on non-tool events are ignored rather than erroring.
- **E17-T2 · Event emission points** — `size:M` · deps: `E17-T1`, `E4-T4` · labels: `type:feature`, `area:ext`, `milestone:m4`
  - AC: `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `SessionStart`, `SessionEnd` and `Stop` fire at the documented points; only `PreToolUse` and `UserPromptSubmit` are blocking.
- **E17-T3 · Execution, payloads and verdicts** — `size:L` · deps: `E17-T2` · labels: `type:feature`, `area:ext`, `risk:security`, `milestone:m4`
  - AC: the event is delivered as JSON on stdin; exit `0` passes, `2` blocks on a blocking event, any other code warns; a spawn error or timeout **blocks** a blocking event and warns otherwise; timeouts are per-event with a documented default.
- **E17-T4 · Project hook approval** — `size:M` · deps: `E17-T3`, `E7-T6` · labels: `type:feature`, `area:ext`, `risk:security`, `milestone:m4`
  - AC: a hook declared by the project runs only after the user approves it once, with the command shown; approval is tied to the project path and to the command text, so an edited command re-prompts; the global hooks file needs no such prompt.
- **E17-T5 · `/hooks` surface and `prx hooks`** — `size:M` · deps: `E17-T4`, `E11-T10` · labels: `type:feature`, `area:cli`, `milestone:m4`
  - AC: `/hooks` lists active and invalid hooks with their event, matcher and source; `prx hooks list|status --json` exposes the same state for automation; an invalid hook names exactly which part is unusable.

### E18 · Instructions and `@`-references — M4

**Goal:** the agent knows the project's rules and the machine it is on, without polluting the cache-stable prefix.

- **E18-T1 · Instruction discovery, precedence and `prx init`** — `size:M` · deps: `E2-T2` · labels: `type:feature`, `area:core`, `milestone:m4`
  - AC: project and global instruction files plus local overrides resolve in a documented precedence, with `@path` imports; a cycle or a missing import is a warning, not a crash.
  - AC: `prx init` writes a starter instruction file and never overwrites an existing one.
- **E18-T2 · Environment summary** — `size:M` · deps: `E2-T2` · labels: `type:feature`, `area:core`, `milestone:m4`
  - AC: one bounded, stable block reports OS, architecture, shell and the versions of detected toolchains; it is byte-stable between turns when nothing changed, so it does not perturb the cache; `[environment] enabled = false` disables it and `offline = true` suppresses futile network retries.
- **E18-T3 · `@`-reference resolution** — `size:L` · deps: `E11-T2` · labels: `type:feature`, `area:core`, `milestone:m4`
  - AC: `@<path>` resolves only when the path exists on disk (so `@mention` and an email address stay literal); a file is wrapped as `<file path="…">` with a size cap and binary files noted rather than dumped; a directory becomes a bounded recursive listing that skips VCS and dependency noise.
  - AC: resolution is asynchronous and user-initiated, does not pass the permission gate, and a failure surfaces as a notice without blocking the turn.
- **E18-T4 · Instruction injection as host-authored context** — `size:M` · deps: `E18-T1` · labels: `type:feature`, `area:core`, `milestone:m4`
  - AC: instructions are injected as a clearly-marked host block outside the cache-stable system prompt, so editing an instruction file mid-session does not invalidate the prompt prefix; a test asserts the system prompt is byte-identical across an instruction edit.

### E19 · CLI surface — M5

**Goal:** discovering and fixing a broken setup takes one command each.

- **E19-T1 · Subcommand routing, usage and exit code 2** — `size:M` · deps: `E14-T1` · labels: `type:feature`, `area:cli`, `milestone:m5`
  - AC: routing covers the v1 verbs from Appendix A §A4.1; an unknown verb or bad flag prints usage and exits `2`; a bare invocation with no TTY prints usage rather than starting a broken session.
- **E19-T2 · `prx setup` wizard** — `size:L` · deps: `E2-T5`, `E3-T5` · labels: `type:feature`, `area:cli`, `milestone:m5`
  - AC: lists configured providers and supports add (OpenAI- or Anthropic-compatible), edit endpoints and model lists, set/refresh key, test connection, choose the default model, and remove.
  - AC: changes are staged and confirmed before writing; saving re-reads the latest config and reports an overlapping concurrent edit as a conflict instead of overwriting it; keys are written to `~/.prx/.env` even when `--local` targets the project config.
- **E19-T3 · `prx doctor` and `doctor capabilities`** — `size:L` · deps: `E7-T5`, `E15-T1`, `E16-T7` · labels: `type:feature`, `area:cli`, `milestone:m5`
  - AC: the default report covers version, OS/arch, cwd, config source paths, resolved default model, sandbox capability and effective bash mode, sandbox write roots, network policy, permission posture and rule counts, MCP/skill/command/hook health, and warnings for ignored project settings and shadowed credentials.
  - AC: `doctor capabilities` emits a stable JSON document with a schema version and a list of issues carrying severity, code, subsystem, source and a remediation; `--live` opts into connecting MCP servers. Exit status is `0`/`1`/`2`.
- **E19-T4 · `prx config` subcommands** — `size:M` · deps: `E2-T3`, `E8-T1` · labels: `type:feature`, `area:cli`, `milestone:m5`
  - AC: `prx config compact-ratio [--local] [VALUE]` shows the effective value and its source, validates the exclusive bounds, and refuses `--local` for a user-global-only key; `prx config reasoning-language` behaves the same way.
- **E19-T5 · Crash capture and local reports** — `size:M` · deps: `E1-T1` · labels: `type:feature`, `area:cli`, `risk:data-loss`, `milestone:m5`
  - AC: an unhandled panic reaching the entry point is saved locally, capped at 10 files with owner-only permissions, with absolute paths reduced and arguments removed; the panic value is never serialised.
  - AC: `prx report list|show|delete` manage them; there is **no** send path in v1; a piped invocation never prompts.
- **E19-T6 · `version` and shell completion** — `size:S` · deps: `E1-T2` · labels: `type:feature`, `area:cli`, `milestone:m5`
  - AC: `prx version` and `prx version --json` report version, commit and build date; `prx completion bash|zsh|fish` emits a completion script generated from the same registry as `/help`.

### E20 · Quality: test harness and end-to-end suite — M0–M5

**Goal:** the tests exist before they are needed, and they test behaviour rather than frames.

- **E20-T1 · Scripted provider and recorded fixtures** — `size:M` · deps: `E3-T1` · labels: `type:test`, `area:quality`, `milestone:m0`
  - AC: a fake provider replays a scripted chunk sequence including tool calls, reasoning, usage, an interruption and an error; fixtures are recorded files, never live calls.
  - AC: each wire kind is additionally exercised against `httptest.Server` with a recorded response body per case — text, tool calls, reasoning, usage, truncation, error (rule [#88]).
- **E20-T2 · End-to-end harness** — `size:L` · deps: `E20-T1`, `E14-T1` · labels: `type:test`, `area:quality`, `milestone:m1`
  - AC: a scenario assembles the real controller against the fake provider and a temp workspace, drives it headlessly, and asserts emitted events, resulting transcript contents, exit code and refusal codes.
  - AC: at least one scenario covers each doc-code from the exit-code table plus a compaction fold and a permission denial.
- **E20-T3 · TUI reducer and render tests** — `size:M` · deps: `E10-T3` · labels: `type:test`, `area:quality`, `milestone:m2`
  - AC: reducer tests feed recorded event sequences and assert rows; render tests assert strings at 40, 80 and 200 columns plus a CJK case; no full-screen golden frames.
- **E20-T4 · Table-driven suites for the deterministic cores** — `size:M` · deps: `E6-T3`, `E8-T3`, `E5-T12` · labels: `type:test`, `area:quality`, `milestone:m2`
  - AC: permission precedence, subject extraction, bash classification, compaction acceptance math, keep-policy budgets and todo invariants each have exhaustive table-driven cases including the negative ones.
- **E20-T5 · Streaming performance benchmark** — `size:M` · deps: `E10-T2` · labels: `type:test`, `area:quality`, `risk:perf`, `milestone:m3`
  - AC: a benchmark measures renders-per-delta and total render time for a 50 K-token streamed answer; the suite fails if renders scale with delta count rather than with batches.
- **E20-T6 · The TUI adds no engine behaviour** — `size:S` · deps: `E20-T2`, `E10-T1` · labels: `type:test`, `area:quality`, `risk:compat`, `milestone:m3`
  - AC: a test (or a documented review gate) asserts that every capability reachable from the TUI is reachable headlessly, guarding G5 and R8.
- **E20-T7 · Go practice audit as a definition-of-done gate** — `size:M` · deps: `E1-T5` · labels: `type:test`, `area:quality`, `milestone:m5`
  - AC: `docs/GO-STANDARDS.md` §5/§6 accounts for all 100 rules: each is either mapped to an enforcing linter or listed as a named audit item — no rule sits in neither tier.
  - AC: `go-best-practices.md` is tracked in git and referenced from `CONTRIBUTING`; the audit protocol is a documented step whose findings are cited as `[#n] path:line` and recorded on the PR.
  - AC: the divergence table (§7) has a reasoned entry for every intentional departure from a rule, including the `init()` removal, the interface allow-list, the `any` options bag and the two sanctioned channel buffers.
  - AC: a `golangci-lint` version bump that changes the mapping updates §5/§6 in the same commit (§8).
  - AC: packages that start goroutines run `goleak` in `TestMain` (rule [#62]).

### E21 · Documentation and onboarding — M5

- **E21-T1 · README, quick start and install** — `size:M` · deps: `E19-T2` · labels: `type:docs`, `area:docs`, `milestone:m5`
  - AC: README explains what the tool is, the 30-second path (install → `prx setup` → `prx`), the headless path, and links the three design documents.
- **E21-T2 · Guide: configuration, permissions, sandbox, themes, keys** — `size:L` · deps: `E12-T3`, `E6-T7` · labels: `type:docs`, `area:docs`, `milestone:m5`
  - AC: a single guide documents every config key with its default, the posture table, the rule grammar with examples, the sandbox behaviour per platform, the theme options and the default keymap.
- **E21-T3 · Annotated `prx.example.toml` and doc-resync checklist** — `size:M` · deps: `E2-T1` · labels: `type:docs`, `area:docs`, `milestone:m5`
  - AC: every key in the example file is commented with its meaning and default, and a test asserts the example parses and matches the schema.
  - AC: a checklist exists for re-syncing this portfolio with upstream Reasonix's vocabulary (R9).
- **E21-T4 · CONTRIBUTING and architecture notes** — `size:M` · deps: `E1-T6` · labels: `type:docs`, `area:docs`, `milestone:m5`
  - AC: contribution guide covers the ticket workflow, the architecture rule, the dependency policy and the test expectations; `docs/ARCHITECTURE.md` is the short version of TDD §6.2 for new contributors.

### E22 · Release and distribution — M5

- **E22-T1 · Build metadata and version injection** — `size:S` · deps: `E1-T2` · labels: `type:chore`, `area:release`, `milestone:m5`
  - AC: version, commit and build date are injected via `-ldflags` from `git describe` and reported by `prx version --json`.
- **E22-T2 · Cross-compile matrix, packaging and checksums** — `size:M` · deps: `E1-T2` · labels: `type:chore`, `area:release`, `milestone:m5`
  - AC: `make cross` produces four archives with a `SHA256SUMS` file; a CI job produces them on a tag.
- **E22-T3 · Install paths and release notes process** — `size:M` · deps: `E22-T2` · labels: `type:docs`, `area:release`, `milestone:m5`
  - AC: the documented install paths (download, `go install`) are verified on a clean machine; a release note template lists user-visible changes and any config migration.
- **E22-T4 · Release checklist and artifact smoke test** — `size:M` · deps: `E22-T2` · labels: `type:test`, `area:release`, `milestone:m5`
  - AC: a smoke test downloads one built artifact on a clean machine, runs `prx version`, `prx doctor` and a headless `-p` turn against a stub endpoint, and asserts exit `0`.

---

## B4. Suggested first sprint

M0's critical path, in dependency order — a shippable walking skeleton in one sprint for one experienced Go developer, longer for a small team working in parallel:

| Order | Tickets | Parallel with |
| --- | --- | --- |
| 1 | `E1-T1`, `E1-T2`, `E1-T5`, `E2-T1` | — |
| 2 | `E1-T3`, `E2-T2`, `E2-T3`, `E3-T1` | — |
| 3 | `E2-T4`, `E2-T5`, `E3-T2`, `E3-T3`, `E9-T1` | E1-T4/T6/T7 |
| 4 | `E3-T4`, `E9-T2`, `E4-T1` | E2-T6, E2-T7 |
| 5 | `E3-T5` (or `E3-T6`), `E4-T2`, `E9-T3` | `E3-T9` |
| 6 | `E4-T3`, `E4-T4`, `E5-T1`, `E5-T3` | `E3-T10` |
| 7 | `E4-T5`, `E5-T4`, `E9-T4` | `E5-T2` |
| 8 | `E14-T1`, `E14-T3`, `E20-T1` | `E9-T5` |
| 9 | `E14-T2`, `E14-T4` (text only), `E20-T2` | `E1-T6`, `E1-T8` |

**Definition of done for M0:** `prx -p "read main.go and summarise it"` completes against a real OpenAI-compatible endpoint, writes a valid event log plus index, resumes with `--continue`, and passes CI on macOS and Linux — all with `read_file`, `ls`, `glob`, `grep` and a permission gate that refuses `write_file` in `ask` without an approver.

---

## B5. Ticket hygiene rules

1. **One ticket, one reviewable change.** If a ticket cannot be reviewed in one sitting, it is `size:L` and should be split before starting.
2. **AC before code.** A ticket with no testable AC is not ready; it is a spike, and spikes produce a decision (recorded here) rather than code.
3. **Every refusal, notice and diagnostic gets a code.** A ticket that introduces user-visible failure without a stable code fails review.
4. **No ticket may add a dependency** without updating `docs/DEPENDENCIES.md` in the same PR.
5. **Anything deferred gets a stub here, not a TODO in code.** A `TODO` in code that is not a numbered ticket is a review failure.
6. **The parity matrix is the definition of "done with the reference".** When a ticket closes, its Appendix A row (or the epic's row) is annotated as covered; an unaccounted row at M5 is a blocking bug.
7. **Every Go change carries a practice audit.** `golangci-lint run` green, the relevant groups of [`GO-STANDARDS.md` §6](./GO-STANDARDS.md#6-audit-checklist-human-decided) walked, and every finding either fixed or accepted with a stated reason — recorded on the PR, not in a chat thread.
8. **A `//nolint` names its reason; a divergence names its rule.** A directive without an explanation fails review (`nolintlint` enforces the first half), and an intentional departure from a baseline rule needs a row in `GO-STANDARDS.md` §7.

---

## R1. Roadmap tier (explicitly out of v1)

Each entry is deferred by decision D1 or D2, not by oversight. Stubs are deliberately larger than v1 tickets — they exist to keep the debt honest, and each will be re-decomposed when it is scheduled.

### R1-1 · Memory facts and recall (was the reference's Context Engine v2)
Fact model with immutable ids, monotonic revisions, scope, type, freshness and expiry; markdown fact files plus a derived index; `remember`/`forget`/`memory` tools; BM25 auto-recall injected as a low-authority suffix before a real user turn with result/character budgets and a relative score floor; archive and recovery. **Depends on:** E8 (projections, compactions), E9 (storage). **Why deferred:** a subsystem, not a feature; without it the agent still works, and a bad memory implementation is worse than none.

### R1-2 · History search
A disposable FTS projection over normalized retrieval tokens across sessions and archives, with `search` and `around` operations and project/global scopes, plus `prx doctor catalogs` and a reindex command. **Depends on:** E9. **Why deferred:** it introduces the first database, which v1 avoids entirely.

### R1-3 · Checkpoints and rewind
Git-free per-turn file snapshots with content-addressed blobs, retention and quota, a durable restore transaction with compensating undo, and — critically — explicit **coverage** and **conflict** reporting so the feature never claims more reversibility than it has. **Depends on:** E9, E5-T2 (`Preflighter`). **Why deferred:** the honesty machinery is most of the work.

### R1-4 · Sub-agents and scheduling
`task`, `read_only_task`, `parallel_tasks`, `fleet` with a dependency DAG, write claims with symlink-safe enforcement, a concurrency/writer scheduler, `complete_subtask` adjudication, result paging, and the `ContextCapsule` record. **Depends on:** E4, E5, E6. **Why deferred:** the reference's own measurements show delegation costs 2.4–4.5× tokens for the same outcome on four task shapes.

### R1-5 · Planner, plan approval FSM, goal mode
A deterministic planning router over pristine user text and trusted turn metadata, a second model in a separate cache-stable session, the plan-mode FSM with epochs and a single approved-plan execution window, plan-submission tools, and the goal mode continuation FSM with blocked-state normalisation. **Depends on:** R1-4's isolation machinery. **Why deferred:** the largest source of cache complexity for the smallest user-visible delta.

### R1-6 · Extension Protocol v2, plugin packages, market
Full-trust sidecar runtime over NDJSON JSON-RPC with interception slots, contributed providers/tools/UI, manifest versioning and drift tests, package install/enable/disable, and a registry browser. **Depends on:** E4, E15. **Why deferred:** the security story alone is an epic.

### R1-7 · Web (serve) and ACP
An HTTP+SSE frontend plus token/password auth and a browser UI; and Agent Client Protocol v1 for editors with session lifecycle, controls and mid-turn steering. **Depends on:** a transport-agnostic controller (already true from E4) and E9. **Why deferred:** D3 chose two surfaces.

### R1-8 · Remote SSH and the provider broker
Bootstrap a detached remote engine, tunnel a loopback port, broker model credentials so the remote host needs no key, SFTP file operations, port forwards, and host-key trust-on-first-use. **Depends on:** R1-7. **Why deferred:** a product of its own.

### R1-9 · Windows support
An honest posture for a platform with no OS sandbox: explicit `bash = "off"` semantics that `doctor` reports, confinement for the in-process file tools, path and console handling (glyph substitution already lands in E13-T4), and CI coverage. **Why deferred:** shipping a "sandbox" that cannot sandbox is worse than not shipping Windows.

### R1-10 · Sandbox escape, MCP OAuth, legacy SSE transport, per-server concurrency
The permission-gated unconfined rerun at the sandbox edge; Protected Resource + AS metadata discovery, DCR, PKCE and refresh for remote MCP; the legacy HTTP+SSE transport; and `concurrency = "serial"` for known stateful servers. **Depends on:** E7, E15.

### R1-11 · Additional tool families
Browser, computer, LSP, notebook editing, AST symbol deletion, code index, charts, `best_of_n`, `advise`, `system_one`, `install_source`, Starlark code mode, spec search and `docs`. Each is an independent ticket; none is required by the core loop.

### R1-12 · Session branching, machine APIs, tasks and schedules
`/tree`, `/branch`, `/switch`, branch metadata and superseded versions; redacted machine-readable session/task/hook JSON with stable ids; the task monitor; durable schedules with grants and budgets; and a durable steering inbox. **Depends on:** E9.

### R1-13 · Updater, telemetry consent, crash-report sending
Signature-verified self-update, opt-in anonymous counters with a consent prompt and a documented data boundary, and explicit crash-report sending. **Depends on:** E22. **Why deferred:** all three are network paths whose only v1 function is convenience.
