# pretty-reasonix — Technical Design Document

| | |
| --- | --- |
| **Status** | Draft for review → becomes the source of truth for the issue backlog |
| **Reference system** | `esengine/DeepSeek-Reasonix` @ `ce84fe9f` (branch `studio`, the 2.x line), analysed read-only |
| **Target** | `pretty-reasonix` — a clean-room Charm(Bubble Tea v2) terminal coding agent |
| **Owner** | samueldaviddelacruz |
| **Backlog** | [`APPENDIX-B-BACKLOG.md`](./APPENDIX-B-BACKLOG.md) — every ticket this document decomposes into |
| **Raw findings** | [`APPENDIX-A-REASONIX-REFERENCE.md`](./APPENDIX-A-REASONIX-REFERENCE.md) — the complete reference inventory |

---

## 0. How to read this document

Three files, one decision record:

| File | Role | Audience |
| --- | --- | --- |
| **`TECHNICAL_DESIGN.md`** (this) | Scope decisions, the reference analysis narrative, the design of record, the engineering plan, the epic map | Anyone who needs to understand *what we are building and why* |
| **`APPENDIX-A-REASONIX-REFERENCE.md`** | The exhaustive inventory of the reference: features, architecture, on-disk formats, CLI surface, tool list, and an honest list of where its UX falls short | Anyone implementing a subsystem who needs to know how upstream did it |
| **`APPENDIX-B-BACKLOG.md`** | Milestones → epics → tickets, with IDs, acceptance criteria, dependencies, sizes and labels | Whoever files and works the tickets |

**Everything in this document is a claim about a specific commit** (`ce84fe9f`, 2.x). The reference moves fast; when a claim matters to a ticket, the ticket states the behaviour we want rather than pointing at upstream code.

---

## 1. Purpose

`pretty-reasonix` is a from-scratch, Charm-based terminal coding agent: it reads a project, edits files, runs commands and tests, asks before dangerous things, and shows its work. It is **inspired by** Reasonix's feature set and **not derived from** its code.

This document does three jobs:

1. **States what we are facing.** Reasonix 2.x is *not* a small CLI — it is ~848,000 lines of Go across 4,892 files, one engine driving four surfaces, with a sandbox, a context-compaction engine, a delegation scheduler and a sidecar extension protocol. Section 5 and Appendix A record that honestly, including the parts we are choosing not to build.
2. **Records the scope decisions** that turn "clone the reasonix CLI" into a buildable product (Section 3).
3. **Decomposes the work** into milestones, epics and tickets (Section 7, Appendix B) so the GitHub project board can be populated directly.

---

## 2. Executive summary

Reasonix is already a Charm application — it ships on `charm.land/bubbletea/v2`, `bubbles/v2` and `lipgloss/v2`. So this project is **not** a port to a new stack; it is a **re-implementation with a fresh UX**, where the interesting engineering is not "can we draw a TUI" but "which of the reference's ~60 tools, four permission postures, OS sandbox, cache-preserving compaction engine, MCP client, skills/hooks/commands system, and determinism-first host policy do we re-derive, and in what order".

The single most valuable idea in the reference is a **separation of authority** that we adopt wholesale:

> The model *proposes* (plans, todo lists, completion claims, delegation prompts, tool calls).
> The host *decides* (planning route, task contract, permission verdict, evidence adjudication, scheduling, closure).
> Every model-facing string is separable from the deterministic policy that consumes it.

Everything else — the tool names, the config keys, the slash-command vocabulary — we mirror for familiarity. The formats, the code and the UX we own.

**v1 shape:** a TUI plus a headless `run`/`-p`, sharing one controller; config-driven provider registry; full local tool belt; permission engine with rule families and postures; a real OS sandbox for shell and file writers; cache-preserving context compaction; append-only versioned session storage with resume; themes; and first-class extensibility through MCP, skills, custom slash commands and hooks.

---

## 3. Scope decisions (locked)

Four decisions were taken with the project owner before design work started. They are the load-bearing constraints of everything below.

| # | Decision | Chosen | Consequence |
| --- | --- | --- | --- |
| D1 | **v1 feature scope** | **Core + extensibility** | v1 is a genuine daily driver: agent loop, providers/streaming, full local tool belt, permission engine + sandbox, sessions/resume, theming — plus MCP, skills, custom slash commands and hooks. Memory/recall, history search, checkpoints/rewind, subagents+fleet, the two-model planner, web/serve, ACP, remote SSH, the desktop app and Extension Protocol v2 are **out of v1** and live in the roadmap tier (§7.5, Appendix B §R1). |
| D2 | **Relationship to upstream** | **Independent implementation, own formats** | No upstream code is copied. We define our own package layout, our own config schema, our own session format and our own state directory. Reasonix is a *feature reference*: read its docs, mirror its vocabulary, ignore its internals — except where a design decision teaches us something (noted explicitly as "adopted from the reference" throughout). |
| D3 | **Surfaces** | **TUI + headless `run`/`-p`** | Two frontends over one controller from day one. No HTTP server, no browser UI, no editor protocol. The controller therefore has to be **transport-agnostic and event-driven** immediately (§6.4), and the headless path doubles as the end-to-end test harness for the TUI. |
| D4 | **Naming** | **Mirror Reasonix's names** | Same subcommand verbs, flags, slash commands and TOML section/key names as Reasonix. Its documentation translates almost directly, and a user's muscle memory carries over. |

### 3.1 Interpretation of D4, and the identity strings it does *not* cover

D4 mirrors the **vocabulary**, not the **identity**. Three strings are ours, because colliding with an installed `reasonix` would be hostile and because D2 gives us our own on-disk formats:

| Thing | Value | Why |
| --- | --- | --- |
| Binary name | `prx` | `reasonix` may be installed; a distinct binary can coexist. |
| State directory | `~/.prx` (override `PRX_HOME`) | Our own format (D2) must not be written into another tool's home. |
| Project config file | `prx.toml` | We mirror the *keys inside* it (`default_model`, `[agent]`, `[permissions]`, `[[providers]]`, …), not the filename. |

Everything else mirrors: `prx run`, `prx setup`, `prx doctor`, `prx mcp add`, `prx --permission-mode plan`, `/rewind`, `/model`, `/theme`, `[agent] compact_ratio`, `[sandbox] allow_write`, `Bash(go test:*)` rules, `--permission-mode`, `-p`, `-c`, `-r`.

> **Cheap to change (flagged, not blocking):** these three strings live in one constants file (§6.11) and a path table, so reversing this interpretation is a mechanical change. If the owner prefers `reasonix`/`~/.reasonix`/`reasonix.toml` for maximum doc fidelity, the tickets do not change — only E2's constants.

**One deliberate compatibility gift:** we *read* `<project>/.claude/commands/` and a `.mcp.json` file, so a Claude-configured project gets its slash commands and MCP servers for free. That is a compatibility *reader*, not a mirrored format writer.

---

## 4. Goals and non-goals

### 4.1 Product goals

| # | Goal | How we know it is met |
| --- | --- | --- |
| G1 | **Fast to first token, fast to first keystroke.** The TUI never blocks on the network; rendering is coalesced and off the critical path. | Typing latency stays flat while a turn streams; a burst of 200 deltas costs one render (§6.12). |
| G2 | **Legible autonomy.** At any moment the user can say what the agent is doing, what it has changed, and what it is waiting for. | The transcript + footer + `/status` answer all three without scrolling archaeology. |
| G3 | **Safe by construction, not by nagging.** Prompt at the edges; run freely inside the box. | Sandbox + permission precedence (`deny` always wins) enforced in the host, not the prompt. |
| G4 | **Cheap to extend.** Adding a provider, an MCP server or a skill is configuration or a directory, never a code change. | New MCP server → one config entry. New model vendor → one `[[providers]]` entry. |
| G5 | **Scriptable to the same standard as interactive.** Every capability reachable from the TUI is reachable headlessly. | `prx -p … --output-format json` and the TUI share one controller; the e2e suite drives the headless path. |
| G6 | **Deterministic where it matters.** Host policy is pure functions over structure. | Planning route, permission verdict, evidence adjudication and todo validation all read types, never prose. |

### 4.2 The UX thesis: what "better UX" concretely means

"Nicer UI and better UX" is not a design brief until it is falsifiable. These are the specific improvements over the reference, drawn from the gaps the analysis actually found (Appendix A §6). Each maps to tickets.

| # | Improvement | Reference behaviour (analysed) | Ours |
| --- | --- | --- | --- |
| U1 | **Rebindable keys** | No central keymap registry: dispatch is an ordered chain of hard-coded handlers. | A `keymap` package: every binding is a named action with defaults, overridable in config, discoverable via `/keys` and validated against conflicts at startup. |
| U2 | **One overlay-ownership rule, enforced by types** | A convention ("modal overlays hide the composer; input-owned overlays keep it") that new overlays must remember, backed by layout tests. | An overlay declares its ownership in the interface (`KeyOwner` / `ComposerVisible`), and the renderer derives composer visibility instead of trusting each panel. |
| U3 | **Inline and fullscreen behave the same** | Inline mode cannot repaint, so folds become pinned/substituted — the same content behaves differently per mode. | One row model where every row declares whether it is *settled*; both modes consume the same settled/unsettled split, so inline is a *sink choice*, not a second rendering semantics. |
| U4 | **A footer that earns its space** | Left: mode + state, right: model/effort; second row: cache %, context, compaction headroom, balance, git. Dense, fixed. | Progressive disclosure: a compact opinionated default, configurable segments, and a togglable expanded panel — the *same* data, sized to the terminal. |
| U5 | **Errors are decisions, not dead ends** | Failures surface as cards; recovery is host-owned with an episode budget. | Same principle, plus a consistent **decision card** component used for approvals, `ask`, sandbox-escape, trust and error-recovery, so every interruption has the same verbs. |
| U6 | **Obvious cost and context** | Cache-hit rate, context use and compaction headroom are telemetry buried in the footer. | A first-class `/status` drawer *and* `/context` breakdown of what fills the window (system, instructions, tools, memory, history, headroom). |
| U7 | **A toolbelt sized to attention** | ~60 model-facing tools, kept manageable by a `use_capability` proxy for optional ones. | Adopt the proxy. Lean always-on surface; everything optional, MCP included, is discovered through the proxy with a stable schema. |
| U8 | **Rewind you can trust, or an honest "no"** | Git-free per-turn snapshots, with **coverage** and **conflict** codes reported rather than assumed. | Adopt the reporting discipline (roadmap tier), and in v1 never claim reversibility we lack: shell side effects are labelled as not covered. |
| U9 | **Discovery without documentation** | `/help` is generated from the same registry that dispatches. | Same, extended: `/help`, `/keys`, `/status`, subcommand `--help` and shell completion all generated from one registry so they cannot drift. |
| U10 | **Progressive disclosure of the session** | Resume picker reads a sidecar index without decoding transcripts. | Adopt, and surface a preview + turn count + model + first prompt in the picker, so choosing a session is informed before it is loaded. |

### 4.3 Non-goals for v1

Explicitly **not** in v1 (each has a home in the roadmap tier, Appendix B §R1):

- Memory facts + BM25 auto-recall; history search catalog; context-engine v2 (projection/restore/migration tooling).
- Checkpoints and `/rewind` — including *any* claim of code reversibility.
- Sub-agents (`task`, `fleet`, `parallel_tasks`, `runAs: subagent` skills) and the two-model planner/coordinator.
- Goal mode with its continuation FSM.
- Web/Browser UI, `serve`, ACP editor integration, remote-SSH, the desktop app, the Extension Protocol v2 sidecar runtime, plugin packages/marketplace.
- Browse/computer/LSP/best-of-n/advisor tool families; notebook editing; `install_source`; Starlark `run_script` code mode.
- Billing display currency, wallet balance, telemetry upload and crash-report sending. (We keep the *local* diagnostics that help a user, not the network paths.)
- Windows. We target macOS and Linux in v1 — matching what the sandbox can actually enforce (§6.8). Windows becomes a follow-up with an honest "bash is unconfined here" story rather than an inherited one.

---

## 5. Reference system analysis (summary)

The full inventory — every feature, config key, tool, flag and on-disk artifact — is in **Appendix A**. This section is the part that changes our design.

### 5.1 What the reference is

One Go engine, four surfaces, one on-disk home:

```
              ┌──────────────────────── one engine ────────────────────────┐
   surfaces   │  TUI (bubbletea v2)   run/-p    web/serve (HTTP+SSE)   acp  │
              └───────────────┬────────────────────────────────────────────┘
                              │   a transport-agnostic controller
              ┌───────────────▼────────────────────────────────────────────┐
   control    │  session/control: turn orchestration, approvals, plan/gate  │
              └───────────────┬────────────────────────────────────────────┘
              ┌───────────────▼────────────────────────────────────────────┐
   runtime    │  agent loop · coordinator · delegation · compaction ·       │
              │  evidence ledger · verdict · task contract · recovery       │
              └───────┬─────────────────┬───────────────────┬──────────────┘
              ┌───────▼──────┐  ┌───────▼───────┐  ┌────────▼──────────────┐
   capability │ tools (~60)  │  │ providers (4  │  │ ext: MCP · extension  │
              │ registry     │  │ wire kinds)   │  │ proto v2 · skills ·   │
              │              │  │ registry      │  │ commands · hooks      │
              └───────┬──────┘  └───────┬───────┘  └────────┬──────────────┘
              ┌───────▼───────────────▼──────────────────────▼──────────────┐
   safety     │  permission (policy) · sandbox (enforcement) · trust         │
              └─────────────────────────────────────────────────────────────┘
              ┌─────────────────────────────────────────────────────────────┐
   state      │  sessions (append-only event log + sidecars) · config ·      │
              │  caches (disposable sqlite projections)                      │
              └─────────────────────────────────────────────────────────────┘
```

Reference scale, for calibration: `internal/{frontend 823, runtime 655, ext 523, contract 479, state 335, session 309, platform 391, assembly 320, tools 203, safety 194, base 173, model 125}` `.go` files.

### 5.2 Design principles worth adopting (and the ones we decline)

| Principle from the reference | Adopt? | Note |
| --- | --- | --- |
| Config- and plugin-driven core; the core knows only interfaces | **Adopt** | Registry by name, no `switch model`. Directly enables G4. |
| Two extension tiers: compile-time built-ins (self-registering) + runtime plugins (stdio JSON-RPC/MCP) | **Adopt the split, not the breadth** | v1 ships tier 1 fully and tier 2 as MCP only. We do *not* adopt its `init()` self-registration: registration is explicit in `internal/assembly` (rule [#3] of the practice baseline, §6.2). |
| Interface-first registries for `Provider` and `Tool` | **Adopt** | With optional capability interfaces discovered by assertion, not by name. |
| Config-visible-only-where-safe: user-global-only keys and *narrow-only* project keys | **Adopt** | A cloned repository must not be able to widen its own sandbox. This is a security property, not tidiness. |
| Cache-first, append-only transcript; rewrite the prefix only at a deliberate cache-reset point | **Adopt** | Cheapest large correctness win in the whole design. |
| Deterministic host policy over model-facing prompt text | **Adopt** | The single most valuable idea (§2). |
| Single static binary, `CGO_ENABLED=0`, cross-compiled | **Adopt** | Keeps distribution boring. |
| Lean dependencies; standard library by default | **Adopt with a documented exception list** | Charm is the point of this project, so Bubble Tea/Lip Gloss/Bubbles are in by design; everything else needs a reason. |
| In-process HTTP kernel that the TUI is a client of | **Decline** | The TUI dials its own engine over `/rt/r1` + SSE. Elegant for multi-surface parity, but we only have two surfaces and D2 says we own the design: a Go interface (a sink), not a socket. |
| Extension Protocol v2 sidecars (17 interception points, slots, full trust) | **Defer** | Powerful and genuinely interesting; far outside v1. Its *slot* concept informs our config merge order. |

### 5.3 Where the reference is strong (we should be at least as good)

1. **The permission model.** `deny > session-allow > ask > allow > read-allowed > fallback`, with subjects extracted generically from tool arguments (`command`, `path`/`file_path`, `pattern`, `source_path`/`destination_path`, `planId`, `origin`, `app`) and Claude-Code-shaped rule families (`Bash(...)`, `Edit(...)`). Deny wins in *every* posture, including YOLO.
2. **The sandbox as a separate layer from permission.** Permission is *policy* (allow/prompt); the sandbox is *enforcement* (writable roots, forbidden reads, network). Shell is jailed by the OS — Seatbelt on macOS, bubblewrap on Linux — and when no backend exists the honest answer is to refuse to run shell, not to run it unconfined.
3. **Cache-preserving compaction.** A single automatic trigger (`context_window × compact_ratio`, default 0.85); below it *nothing* is rewritten; at it, one summary transaction of `stable prefix + digest + recent verbatim tail`, with user turns protected by budget and `[[keep]]` overrides, and failures recorded rather than retried silently.
4. **Evidence-based completion.** A step is only signed off when the cited command has a *host-recorded* receipt or the cited paths have real write/read receipts. The host never raises a status.
5. **Coalesced streaming and settled prefix.** A burst of deltas costs one render; only text up to the last blank line *outside a fenced block* is "settled" and printable, which is what makes streaming look calm.
6. **Cross-platform terminal hygiene.** Grapheme-aware width, 256-colour fallbacks pre-resolved, glyph substitution for consoles that cannot draw a rune, mouse off by default over SSH.

### 5.4 Where we will deliberately differ

| Area | Reference | pretty-reasonix v1 | Why |
| --- | --- | --- | --- |
| Frontend transport | HTTP+SSE kernel the TUI is a client of | In-process Go `Sink` interface | Two surfaces, not five; a socket buys nothing and costs latency and tests. (U10-adjacent; recorded in §6.4.) |
| Survivability machinery | Recovery branches, lease handoff, superseded versions, damaged-tail salvage, in-flight-turn repair | Simple: append-only log, single-writer lock, truncate-to-last-good-record with a `.damaged` sidecar | We are not yet multi-window. Keep the *invariant* (never lose committed history) and skip the machinery until the surface demands it. |
| Tool breadth | ~60 tools | ~24 in v1 | Every tool is model-visible tokens and prompt surface. Add when a real workflow needs it. |
| Delegation | `task`/`fleet` with a write-claim scheduler and 5-layer delegation vocabulary | Deferred | Not in D1's tier, and the reference's own measurements show delegation is usually a token *loss* (2.4–4.5× for identical outcomes). |
| Planner | Deterministic route → second model in a separate session | Deferred | Same reason; also the biggest source of cache-complexity. |
| Extension protocol | Full-trust sidecars with interception slots | Deferred to roadmap | v1 extensibility is MCP + skills + commands + hooks, which covers the real use cases. |
| Windows | Ships, with bash unconfined and no OS sandbox | Out of v1 | Rather than inherit an unsafe default, we scope to the two platforms where the sandbox is real. |

---

## 6. pretty-reasonix design

### 6.1 Technology choices

| Concern | Choice | Rationale |
| --- | --- | --- |
| Language | **Go** (toolchain ≥ 1.25; the module targets 1.27) | Single static binary, `CGO_ENABLED=0`, cross-compiles; the reference proves the ergonomics. |
| TUI | **`charm.land/bubbletea/v2`**, **`bubbles/v2`**, **`lipgloss/v2`**, `charmbracelet/x/ansi`, `colorprofile` | The stated point of the project. |
| Syntax highlighting | `alecthomas/chroma/v2` behind an interface, with a plain fallback | Rich rendering without a hard dependency on it at render time. |
| Markdown | `yuin/goldmark` (GFM tables) | Same as reference; mature. |
| Diffs | `aymanbagabas/go-udiff` for the built-in renderer | Purpose-built, small. |
| Width math | `rivo/uniseg` + `mattn/go-runewidth`, wrapped in our own `cells` package | Width bugs are the #1 TUI defect class; it needs one owner. |
| Config | `BurntSushi/toml` | Only accepted config dependency in the reference too. |
| Linting & formatting | **`golangci-lint` v2.14.0** (pinned) with the `gofmt`/`goimports` formatters, configured in [`.golangci.yml`](../.golangci.yml) | Enforcement tier 1 of the Go practice baseline (§7.2). Every entry cites the rule it enforces; the set is curated, not a preset. |
| Code standards | [`go-best-practices.md`](../go-best-practices.md) — the numbered rules — plus [`docs/GO-STANDARDS.md`](./GO-STANDARDS.md) — the enforcement map and the audit protocol | Two tiers: what a linter can decide is a gate; what it cannot decide is a numbered post-implementation audit item. |
| SQLite | **Not in v1** | The reference's sqlite stores are all *disposable projections*. Without history search / usage catalogs we need no database at all. |
| Keyring | Not in v1; credentials are a `0600` file in `~/.prx` | Simplicity; the file is the reference's primary source anyway. |
| Shell parsing (for permission subjects) | `mvdan.cc/sh/v3/syntax` | Parsing beats pattern-matching for `Bash(...)` subject extraction and `:*` prefix rules. |
| HTTP client | `net/http` from the standard library, wrapped in `internal/httpclient` for retry/SSE/proxy policy | One place owns retry, backoff, idle watchdog and proxy. |

Every dependency above must be pure Go and must not break `CGO_ENABLED=0`. The policy, the approved list with a reason per module, what is deliberately refused, and the review checklist all live in [`DEPENDENCIES.md`](./DEPENDENCIES.md) (ticket **E1-T4**); `make pure-go` is the mechanical check.

### 6.2 Package layout and the dependency rule

```
pretty-reasonix/
├── cmd/prx/main.go                    # entry: build info, crash capture, cli.Run
├── internal/
│   ├── contract/                      # interfaces + wire types ONLY; imports nothing of ours
│   │   ├── provider/                  #   Provider, Request/Message/Chunk/ToolCall/Usage
│   │   ├── tool/                      #   Tool + optional capability interfaces + Registry
│   │   ├── permrule/                  #   Rule grammar (parse/format/match)
│   │   └── event/                     #   the frontend event vocabulary (§6.4)
│   ├── architecture/                  # the dependency-rule test (E1-T3); no production code
│   ├── config/                        # TOML schema, defaults, resolution, paths, trust store
│   ├── provider/
│   │   ├── openai/                    #   kind="openai"   (chat completions, SSE)
│   │   ├── anthropic/                 #   kind="anthropic" (Messages API, SSE)
│   │   └── responses/                 #   kind="responses" (Responses API)
│   ├── model/                         # model reference resolution, windows, output caps, pricing
│   ├── httpclient/                    # retry/backoff, SSE reader, idle watchdog, proxy
│   ├── tool/
│   │   ├── builtin/                   # read_file, write_file, edit_file, … (registered at boot)
│   │   └── (registry lives in contract/tool)
│   ├── perm/                          # Policy, Gate, Approver, subject extraction
│   ├── sandbox/                       # Spec + seatbelt_darwin.go / bwrap_linux.go / none
│   ├── session/                       # transcript model, store, leases, resume, Meta sidecar
│   ├── agent/                         # the turn loop, tool batching, ContextManager
│   ├── compaction/                    # compaction: plan, digest, keep policy, projections
│   ├── ext/
│   │   ├── mcp/                       #   MCP client: stdio + http transports
│   │   ├── skill/                     #   skill discovery + inline execution
│   │   ├── command/                   #   custom slash commands
│   │   └── hook/                      #   shell hooks + event matchers
│   ├── assembly/                      # boot: config → registries → controller → frontend
│   ├── frontend/
│   │   ├── cli/                       #   subcommand routing, flags, exit codes, setup/doctor
│   │   ├── tui/                       #   the Bubble Tea app (§6.12)
│   │   ├── run/                       #   headless run/-p (§6.14)
│   │   └── termrender/                #   stateless drawing: palette, markdown, diffs, cards
│   ├── i18n/                          # message catalog
│   └── version/                       # ldflags-injected build info
├── docs/                              # this document + appendices + guide
├── examples/                          # a reference MCP stdio server (test fixture + docs)
├── Makefile
└── prx.example.toml
```

**The dependency rule (acyclic, enforced by a test):**

```
frontend/{tui,run} ─┐
                    ├─→ assembly ─→ agent ─→ {tool, provider, ext} ─→ {session, perm, sandbox, config} ─→ contract
frontend/cli ───────┘
```

Rules:
- `contract` imports nothing of ours, and capability packages never import each other. **Registration is explicit**: `assembly` imports the parents *and* the concrete kinds, then calls their `Register` functions. So the real edge is `assembly → {provider, tool, ext}`, with no side-effecting blank imports and **no `init()` anywhere** (rule [#3]). A duplicate kind returns an error instead of panicking (rule [#48]).
- `termrender` imports no session state and no `agent` — it renders bytes from values, exactly like the reference's stateless renderer. This is what makes the TUI testable.
- Nothing outside `frontend/*` writes to stdout/stderr or calls `os.Exit`; library code wraps errors with `%w` and returns.
- `sandbox` and `perm` never import `agent`: the agent asks a `Gate`, and a `Sandbox.Spec` is data.
- Ticket **E1-T3** adds `internal/architecture/`, whose test fails on an import edge violating this graph *and* on a package that declares no layer, so the rule cannot silently lose coverage.
- No package may be named `utils`, `common`, `base`, `helpers`, `misc`, `shared`, `types` or `consts`; `depguard` enforces it (rule [#13]).

### 6.3 Core contracts

```go
// internal/contract/provider — transport-agnostic. Everything here is data.
type Provider interface {
    Name() string
    Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

type Factory func(cfg Config) (Provider, error)
func Register(kind string, f Factory) error  // duplicate kind is an error, not a panic [#48]; called from internal/assembly, never from init() [#3]
func New(kind string, cfg Config) (Provider, error)
func Kinds() []string

type ChunkKind int
const (
    ChunkText ChunkKind = iota
    ChunkReasoning
    ChunkToolCallStart
    ChunkToolCallArgsDelta
    ChunkToolCall
    ChunkUsage
    ChunkDone
    ChunkError
)

type Chunk struct {
    Kind      ChunkKind
    Text      string
    ToolCall  *ToolCall
    Usage     *Usage
    Err       error
}
```

```go
// internal/contract/tool — the model-facing surface.
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage          // JSON Schema for parameters
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Optional capabilities are discovered by TYPE ASSERTION, never by name lookup.
type ReadOnly interface{ ReadOnly() bool }        // may run in a parallel batch
type Sequential interface{ RunsSequentially() bool } // opts out of batching (stateful)
type PlanSafe interface{ PlanModeSafe() bool }     // may run while planning
type SnipHinter interface{ SnipHint() Snip }       // geometry for output truncation
type Paged interface{ Paged() bool }               // windows instead of truncating
type Barrier interface{ DecisionBarrier() bool }   // stops the batch: needs a human
type Preflighter interface{ Preview(ctx, args) (Preview, error) } // for diff previews/checkpoints
```

```go
// internal/perm — pure policy. No I/O.
type Decision int
const (Allow Decision = iota; Ask; Deny)

type Policy struct {
    Mode            Decision     // writer fallback when nothing matches
    Allow, Ask, Deny []permrule.Rule
    SessionAllow    []permrule.Rule
    ReadOnly        bool         // posture: refuse every non-read
    AllowDynamicBash bool
}

// Decide is pure; the Agent consults a Gate, never a Policy, at execute time.
func (p Policy) Decide(toolName string, readOnly bool, args json.RawMessage) Decision

type Gate interface {
    Decide(toolName string, readOnly bool, args json.RawMessage) Decision
    Approve(ctx context.Context, req ApprovalRequest) (Approval, error) // Ask resolution
}
```

```go
// internal/session — the transcript is append-only; a fold is an explicit event.
type Message struct {
    Role       Role
    Content    string
    RawContent string        // full body when Content was bounded for the model
    ToolCalls  []ToolCall
    ToolCallID string
    Hidden     bool          // injected, not user-authored
}

type Store interface {
    Open(id string) (*Session, error)
    List() ([]Meta, error)                       // reads the sidecar index only
    Append(ctx context.Context, sess *Session, msgs []Message) error
    Replace(ctx context.Context, sess *Session, msgs []Message, reason string) error
    Lease(id string) (Lease, error)              // single writer
}
```

```go
// internal/agent — the loop. A Runner is anything the CLI can drive.
type Runner interface {
    Run(ctx context.Context, input string) error
}
```

### 6.4 The event model — the only interface between engine and frontend

Two frontends (D3) must not be able to drift. So the controller emits **typed events** and a frontend is a sink; there is no HTTP, no shared mutable UI state, no second rendering path.

```go
// internal/contract/event
type Event interface{ eventKind() string }

type TurnStarted struct{ TurnID, Input string }
type TextDelta struct{ TurnID, ItemID, Text string }
type ReasoningDelta struct{ TurnID, ItemID, Text string }
type MessageCommitted struct{ TurnID, ItemID string; Message provider.Message }
type ToolDispatched struct{ TurnID, ItemID, CallID, Name string; Args json.RawMessage; ReadOnly bool }
type ToolProgress struct{ CallID string; Phase string; Preview string; Elapsed time.Duration }
type ToolResult struct{ CallID string; Output string; Truncated bool; Err error; DurationMs int }
type ApprovalRequested struct{ CallID string; Name, Subject, Preview string; Options []ApprovalOption }
type ApprovalDecided struct{ CallID string; Decision Decision; Scope GrantScope }
type AskRequested struct{ ItemID string; Questions []Question }
type Notice struct{ Level NoticeLevel; Code, Text string }     // includes every typed refusal
type TodoStateChanged struct{ Todos []Todo }
type CompactionStarted struct{ TurnID string; Reason string }
type CompactionCommitted struct{ TurnID string; Digest string; TokensBefore, TokensAfter int }
type UsageReported struct{ TurnID string; Usage provider.Usage; Cost *CostQuote }
type TurnDone struct{ TurnID string; Status TurnStatus; Usage provider.Usage; Readiness *Readiness }

type Sink interface{ Emit(Event) }
```

Rules that make this worth having:
1. **Events are the API.** The TUI reducer, the headless printer and (later) any file recorder consume the same stream. `--output-format stream-json` becomes a three-line sink, not a second code path.
2. **Events carry structure, not prose.** A notice carries a stable `Code` (e.g. `permission.deny_rule`) so the UI can localise, style and offer a remedy, and so tests can assert on codes rather than strings.
3. **The frontend never blocks the engine.** `Sink.Emit` must not block; the TUI's sink is a buffered channel drained by a `tea.Cmd`, with an explicit drop policy and a "N events dropped" notice rather than backpressure on the model. The capacity is a **named constant with its size and policy in a comment** — one of the two sanctioned buffers under rule [#67] ([`GO-STANDARDS.md` §7](./GO-STANDARDS.md#7-deliberate-divergences-and-sanctioned-exceptions)), never a bare number passed to `make`.
4. **Approvals are events with an answer channel**, not callbacks into the UI. Headless answers them synchronously; the TUI answers them from the decision card.

### 6.5 The turn loop

One user turn, with the authority split made explicit:

```
Run(ctx, input):
  turn := beginTurn()                    # reset turn state; freeze model/effort/preset
  policy := policy.Derive(input)          # DETERMINISTIC; frozen for the whole turn
  append(userTurn(input) + policy.Block)  # the block is host-authored, marked as such
  for step := 0; step < maxSteps; step++:
      req   := buildRequest(messages, toolSchemas)   # cache-stable prefix
      chunks := provider.Stream(ctx, req)
      text, reasoning, calls, usage := foldChunks(chunks)   # emit deltas as they arrive
      if ctx.Err() != nil: interrupt(); break
      if len(calls) == 0:
          readiness := finalReadiness()      # did the declared checks actually run after the last write?
          emit(TurnDone{...}); break
      for segment := range batchSegments(calls):   # contiguous parallelisable calls in one segment
          segment.run()                            # parallel iff all ReadOnly && !Sequential, cap 8
          if segment.refusedByBarrier(): break
      append(toolResults(calls))               # errors are fed back, never fatal
```

Host-side rules inside the loop:

- **Batching.** A batch is cut at the first `DecisionBarrier` tool; then partitioned into *contiguous* parallelisable runs (read-only, not sequential) executed with `maxParallel = 8`, with writers, unknowns and sequential tools executed serially so write-before-read provider order is preserved. The parallel run uses `golang.org/x/sync/errgroup` (rule [#73]) and writes results **by index into a pre-sized slice**, never by appending from goroutines (rule [#69]).
- **Interruption.** `ctx` threads everywhere; a cancelled turn writes a bounded recovery record that is folded into the *next* user turn, so the user's next message does not start from amnesia.
- **Retries.** Provider-level retry replays the *identical frozen request* — no synthetic messages — with exponential backoff and jitter, bounded. A mid-stream cut surfaces as a typed `StreamInterruptedError` the loop may replay once.
- **Idle watchdog.** A silent stream for `idle_timeout_seconds` (default 300) is treated as dropped.
- **Reasoning effort.** `--effort`, a model's `supported_efforts` and its `default_effort` resolve to one level before the request is built (`internal/model`): the request wins, then the default, then nothing at all. A level the model does not offer is an error rather than a quiet fall back to the default, because someone who asked for maximum effort and received the ordinary level would never find out — the answer would just be less thorough than they paid for. Each kind maps the resolved level onto its own wire field; a protocol with no such field declares no supported levels, and a request for one is refused with that reason.
- **Readiness.** After the model stops, the host checks that declared verification commands ran *after* the last write. Unmet readiness does not fake a success: it is reported, and the run's exit status says so (§6.14).
- **Step budget.** `--max-steps` bounds the loop; exhausting it arms exactly one grace round so the model can close out, then ends the turn with a resumable status.
- **What the user sees** is entirely §6.4 events, which is why the TUI and `prx -p` cannot diverge.

### 6.6 Tool framework and the v1 toolbelt

Canonicalisation and bounding, both adopted from the reference because both are cheap and both prevent whole bug classes:

- Schemas are **inline JSON literals** in `Schema()`, canonicalised once on registry insert and cached; a generated `docs/tool-contract.md` is asserted against the same code path by a test, so the documented surface cannot drift from the real one.
- Tool output is bounded **once, on first entry to the model**: `Content` is the stable ≤32 KiB visible form and `RawContent` keeps the full body only when they differ. Oversized results spill to a file and are addressed by a pager. Maintenance never rewrites old tool bodies.
- `Execute` parses its own args and returns errors as data; the model self-corrects. A validation error is a normal tool result.

**v1 toolbelt (always-on surface):**

| Tool | Purpose | R | Notes |
| --- | --- | --- | --- |
| `read_file` | Line-numbered read with offset/limit | ✓ | `Paged`, `SnipHint` |
| `write_file` | Whole-file write, creates parents | | Confined to write roots; refuses to clobber unread changes |
| `edit_file` | Single unique-string replace | | `old_string` must occur exactly once |
| `multi_edit` | Atomic ordered edit list | | All-or-nothing rewrite |
| `move_file` | Move/rename | | Destination must not exist; both ends checked against roots |
| `delete_range` | Delete a line span by unique anchors | | Returns a unified diff |
| `ls` | Directory listing (recursive optional) | ✓ | Skips VCS/dependency noise |
| `glob` | Path glob (`**`, `*`, `?`, `[]`) | ✓ | Bounded result count + timeout |
| `grep` | Regex search | ✓ | Bounded matches + timeout |
| `bash` | Run a command (sandboxed) | | Foreground timeout; background mode |
| `bash_output` | Read new output of a background job | ✓ | Sequential |
| `kill_shell` | Terminate a background job | | |
| `wait` | Block until background jobs finish | ✓ | Sequential |
| `todo_write` | Replace the task list | ✓ | Sequential; one `in_progress` (§6.9.3) |
| `complete_step` | Sign off one step with evidence | ✓ | Sequential; host-verified; refused in plan mode |
| `ask` | Ask the user 1–4 questions | ✓ | `DecisionBarrier` — stops the batch |
| `await_user` | Hand the turn back with the list open | ✓ | `DecisionBarrier` |
| `compress` | Summarise part of the conversation | ✓ | Sequential; user-requested |
| `recall` | Re-read folded content by `#n` or query | ✓ | |
| `context_budget` | Report remaining context room | ✓ | |
| `web_fetch` | Fetch a URL as text | ✓ | SSRF guard |
| `use_capability` | Search/inspect/call deferred capabilities | ✓ | Sequential; the MCP and optional-tool proxy (§6.15) |
| `read_skill` / `run_skill` | Load / run an inline skill | ✓ / | Subagent-executed skills are roadmap |
| `slash_command` | Invoke a project slash command | ✓ | |

Registered at boot rather than as builtins, because they need host services: `history`-style readers are roadmap; `write_file`/`edit_file` need the write-root resolver; `bash` needs the sandbox spec.

**Explicitly deferred tools** (roadmap tier, each with a ticket): `delete_symbol`, `notebook_edit`, `code_index`, `render_chart`, `conclude_blocked`, `submit_plan`/`review_report` (need plan mode), `apply_isolated`/`discard_isolated`, every `task`/`fleet`/`parallel_tasks`/`read_subagent_result`/`list_subagents`, `advise`/`best_of_n`/`system_one`, `browser_*`/`computer_*`/`lsp_*`, `web_search`, `docs`, `install_source`, `run_script`, `memory`/`remember`/`forget`, `list_sessions`/`read_session`.

### 6.7 Permission engine

Adopted in full; it is the reference's best-designed subsystem and it is what makes "safe by construction" (G3) true rather than aspirational.

**Grammar.** A rule is one of:
- `Tool` — matches any call in that family (`Bash`, `Edit`, or a concrete tool name).
- `Tool(specifier)` — matches when the call's extracted *subject* matches the specifier. `Bash(go test:*)` is a prefix approval; `Bash(npm run build)` is exact; `Edit(docs/**)` is a path glob.
- `Tool=literal` — legacy exact form kept for round-tripping; metacharacters are ordinary.

**Subjects are extracted generically** from the call's JSON args — `command` (bash), `path`/`file_path` (file tools), `pattern` (grep/glob), `source_path`/`destination_path` (move, both checked), `planId`, `origin`, `app` — so no tool needs to change to become rule-addressable.

**Precedence, in order:** `deny` → session grant → `ask` → `allow` → read-allowed → "subject requires a human" → posture fallback. **`deny` wins in every posture, including bypass.** Read-only postures refuse non-reads before any allow rule is consulted.

**Approver scopes.** A prompt offers: allow once · allow for this session (scoped) · always allow (persisted) · deny. Persisted rules are shaped conservatively: a bash command-prefix rule when safe (`Bash(go test:*)`), a shared `Edit(<path>)` rule for all file-mutating tools, and an exact literal when no safe prefix exists.

**Dynamic shell is stricter.** Parameter/arithmetic expansion, assignments, heredocs, redirects and globs cannot inherit a bare or prefix allow; remembered approvals for them are exact literals. Command/process substitution, a dynamic command name, `eval`, `source`, `sh -c` and similar nested/indirect forms require a human in interactive Ask/Auto, and only an identical exact grant or bypass resolves them. An advanced `[permissions] allow_dynamic_bash` opt-in relaxes this for the fallback class only — explicit `ask`/`deny` still win.

**Headless resolution (no TTY).** Ask/manual fails closed; Auto allows the ordinary writer fallback; bypass runs ordinary calls but still honours deny rules, the sandbox and tools requiring fresh human approval. A refusal never changes the exit code but is reported (§6.14).

**Recursive-delete guard.** Independently of postures, the shell layer hard-refuses recursive deletion of the home directory, a filesystem root, an ancestor of the workspace, or a path outside the declared roots, with typed codes (`shell.delete_sequence`, `shell.destructive_target`, …) surfaced as notices with a remedy.

### 6.8 Sandbox and workspace trust

Deliberately separated from permission: `perm` answers "may this run?", `sandbox` answers "what can it actually reach?".

- **File writers** are confined to `workspace_root` (default: cwd) + `[sandbox] allow_write`, resolved to absolute, symlink-free paths so neither `..` nor a symlink inside a root can tunnel out. `forbid_read` blocks reads/listings/searches, with `${VAR}` expansion so `${HOME}/.ssh` can be named.
- **Shell** is jailed by the OS: Seatbelt (`sandbox-exec -p <SBPL>`) on macOS, bubblewrap on Linux. Writes are limited to the same roots plus toolchain temp/cache roots; `forbid_read` roots are denied for read *and* write; network is denied unless `[sandbox] network = true`.
- **When no sandbox backend exists, shell refuses to run.** The honest failure is an error the model can adapt to; the alternative — silently running unconfined — is how "sandbox" becomes a lie. A permission-gated "run unconfined once" escape is a roadmap ticket.
- **Workspace trust.** Because `auto` is only defensible inside a sandbox, the default posture is: `auto` **only if** the OS sandbox confines shell writes *and* the user has trusted this folder; otherwise `ask`. Trust is per-path, recorded in `~/.prx`, revocable with `prx trust --revoke`, and a project's own files cannot record it. Headless runs never prompt.
- **Write-root identity is pinned** for the life of a session: if a root's real path changes (mount swap, symlink retarget), the session fails closed rather than writing somewhere new.

### 6.9 Context management

A coding agent that cannot compact is a demo. v1 therefore ships compaction, in the reference's cache-first shape but with a smaller surface.

#### 6.9.1 The rule

```
triggerTokens = floor(context_window × compact_ratio)     # compact_ratio default 0.85
```

`context_window × compact_ratio` is the **only** automatic trigger. Below it: no summary, no prune, no projection, no sidecar write — because *any* rewrite invalidates the prompt cache from that point on. At it: exactly one summary transaction.

#### 6.9.2 The transaction

```
stable prefix  (system prompt + pinned first user turn if small)
+ one structured digest   (Standing facts · Goal · Decisions · Files · Commands · Errors · Pending)
+ recent verbatim tail
```

- **Acceptance:** the candidate must be ≤ 50 % of the window, strictly smaller than the source, and below the trigger. Candidates are not padded toward the ceiling; typical landings are 10–30 %.
- **What survives verbatim, always:** the system prompt; the first user turn when small enough to be a brief; the recent tail; error-carrying tool results; anything matched by the keep policy; the active turn's opening user turn.
- **User turns get budgeted protection**, oldest-first: one turn may spend up to 1500 tokens and all of them together `min(8192, window × 5 %)`. The asymmetry is deliberate: code stays re-derivable from the workspace, a constraint stated at turn 4 ("do not change the public API") exists nowhere else. A turn the budget cannot cover can be pinned with a `[[keep]]` prefix.
- **The drop is never silent.** Compaction telemetry carries user-kept/user-dropped counts; a committed checkpoint that folded one of the user's turns emits a warning naming `[[keep]]`.
- **Each fold re-derives from the canonical transcript**, never from the previous digest, so digests do not chain and repeated compaction does not compound summariser drift.
- **Failure is recorded, not retried.** A generation that failed records a typed blocked receipt and does not pay twice; a manual `compress` may retry.
- **The canonical transcript is never rewritten.** Compaction writes a *projection*. Everything folded stays recoverable — that is what makes `recall` and `compress` honest.
- **The provider boundary strips internal records.** `ModelMessages` removes `RawContent` and host bookkeeping so serialisation and cache hashes never see them, while the stored projection keeps them so the *next* fold can still classify a failure properly.

#### 6.9.3 Todos, evidence and readiness (the determinism half of the loop)

- `todo_write` replaces the whole list. Contract: at most two levels (a phase and its sub-steps); **exactly one** `in_progress` item; sub-steps complete in order; a level-1 item with no phase above it is rejected; `step_id`s are copied verbatim by the model and are the identity used by `complete_step`. The host rejects a list that removes the in-progress item, moves it backwards, breaks the completed prefix, or newly completes an item with no matching `complete_step` receipt.
- `complete_step` signs off **one** step and requires at least one evidence item (`verification` | `review` | `diff` | `files` | `manual`). The host then **adjudicates**: a cited `verification` must name a command with a real successful receipt; `diff`/`files` must name paths with real write/read receipts; `manual` cannot stand alone once receipts exist. The host never raises a status it cannot prove, and it advances the list itself.
- **Readiness** is the turn-level version: if the model declares checks and the last write happened after they ran, the run reports unmet readiness instead of success.

### 6.10 Sessions and persistence (our own format)

Append-only, versioned, single-writer. Ours, per D2 — but we keep the reference's *invariant*: committed history is never lost, and unknown future formats are refused rather than misread.

```
~/.prx/
├── config.toml            # 0644  our config (keys mirror Reasonix)
├── .env                   # 0600  provider secrets (values only, referenced by api_key_env)
├── trust.json             # 0600  trusted workspace paths
├── commands/              #       global custom slash commands
├── skills/                #       global skills
├── settings.json          # 0600  global hooks
├── sessions/
│   ├── <id>.jsonl              # 0600  canonical transcript (one JSON object per message)
│   ├── <id>.events.jsonl       # 0600  authoritative append/replace event log
│   ├── <id>.index.json         # 0644  index: turns, title, preview, model, counts, digest, writer
│   ├── <id>.lock               #       single-writer lease
│   └── <id>.outputs/           #       spilled tool output bodies
└── logs/                       #       local diagnostics (rotated, bounded)
```

**The event log** has exactly two record kinds, which keeps replay trivially checkable:

```json
{"type":"append","revision":12,"base_revision":11,"message_index":48,
 "messages":[…],"content_digest":"…","writer_id":"…","created_at":"…"}
{"type":"replace","revision":13,"base_revision":12,"message_index":0,
 "messages":[…],"reason":"compaction","content_digest":"…"}
```

- `append` requires `message_index == current length` (the chain check) — that is what makes torn writes detectable.
- `replace` marks a deliberate history rewrite (compaction) and is the only way the prefix changes.
- Every record is fsynced. Replay is **tolerant**: it stops at the first unparseable or non-chaining record and returns the valid prefix; the store then truncates to the last good boundary and moves the discarded bytes to `<id>.events.jsonl.damaged`. An **unknown schema version or record type is a hard error** — a newer writer owns the file, and guessing would corrupt it.
- The log folds into a single `replace` when it grows past a threshold (128 MiB) or 4× live content, atomically via temp+rename.

`<id>.index.json` exists so the session picker can list sessions with preview, turn count, model and timestamps **without decoding any transcript** (U10). It is a cache: a mismatch against the log's digest is repaired, never trusted over the log.

**Single writer.** A lease file is taken before a session is opened for writing; a second writer gets a typed error naming the holder, and `--copy` is the documented way to continue someone else's session without touching it.

**Crash repair.** The index records an in-flight turn (start index, start revision, start digest). On resume, an in-flight turn whose commit digest is absent is truncated back to the start; one whose commit *is* on disk is kept. Simple, and it never invents history.

### 6.11 Configuration and paths

Our schema; the reference's **key names**, so its docs and our docs describe the same knobs.

```toml
default_model = "deepseek"
language      = ""              # empty = auto-detect from $LANG / $PRX_LANG

[ui]
theme         = "auto"          # auto | dark | light
theme_style   = ""              # accent palette; empty = default for the mode
cursor_shape  = "bar"           # bar | block | underline
show_turn_usage = true
commandmode   = ""              # "" | vi   (user-global only)
statusline    = "compact"       # compact | full | off

[agent]
system_prompt       = ""
system_prompt_file  = ""
temperature         = 0.0
reasoning_language  = "auto"    # auto | en | zh
compact_ratio       = 0.85      # the sole automatic compaction trigger
max_output_tokens   = 0         # 0 = automatic ladder; negative = omit the wire limit
output_style        = ""
keymap              = {}        # U1: action → key sequence

[providers]                     # one vendor endpoint may list several models
[[providers]]
name = "deepseek"
kind = "anthropic"              # openai | anthropic | responses
base_url = "https://api.deepseek.com/anthropic"
models = ["deepseek-flash", "deepseek-pro"]
default = "deepseek-flash"
api_key_env = "DEEPSEEK_API_KEY"
context_window = 128000
# request_url, max_output_tokens, idle_timeout_seconds, model_overrides, prices{}

[tools]
enabled = []                    # empty = all built-ins
bash_timeout_seconds = 120
protect_changed_files = true
mcp_startup_timeout_seconds = 30
mcp_call_timeout_seconds = 300

[permissions]
mode  = "ask"                   # ask | allow | deny  (writer fallback)
deny  = ["Bash(rm -rf*)"]
allow = ["Bash(git status:*)"]
ask   = []

[sandbox]
workspace_root = ""             # empty = cwd
allow_write    = []
forbid_read    = ["${HOME}/.ssh"]
bash           = "enforce"      # enforce | off  (no backend ⇒ enforce refuses)
network        = true

[skills]
paths = []
disabled_skills = []

[environment]
enabled = true
offline = false

[[plugins]]                     # MCP servers
name = "example"
type = "stdio"                  # stdio | http
command = "prx-mcp-example"
args = []
```

**Resolution order:** `flags > ./prx.toml > ~/.prx/config.toml > built-in defaults`.

**Two safety properties, adopted from the reference because they are security, not style:**

1. **User-global-only keys** a project file cannot set: `[serve]`-equivalents, `[remote]`, `[telemetry]`, `[ui].commandmode`, credential/store paths, `[agent] system_prompt*` is *allowed* but `[sandbox]`/`[permissions]` cannot be *widened*.
2. **Narrow-only project keys.** A project may tighten `[sandbox]` and `[permissions]` (add `deny`, add `forbid_read`, restrict roots) but never loosen them. Violations are not silently dropped: they are reported as `IgnoredProjectSettings` notices and listed in `prx doctor`.

Secrets: provider entries name an environment variable; values live in `~/.prx/.env` at `0600`. The environment variable is **removed from every child process's environment** before a tool runs, so a shell command cannot read the user's model key.

Identity constants (`prx`, `~/.prx`, `prx.toml`, `PRX_*`) live in one file, `internal/config/identity.go` (§3.1).

### 6.12 The TUI

The centrepiece, and where the "better UX" claim is either earned or not.

#### 6.12.1 Shape

```
internal/frontend/tui/         the Bubble Tea app        (stateful: model, reducer, viewport)
internal/frontend/termrender/  stateless drawing         (palette, markdown, diffs, cards, width)
```

`termrender` holds **no session state**: for the same input values it produces the same bytes. The TUI composes its output and owns the reducer. This split is adopted from the reference because it is what makes the UI testable without a golden-frame harness.

#### 6.12.2 The row model (U3)

The transcript is a list of typed rows, and every row knows whether it is **settled** — final and printable.

```go
type RowKind int
const (
    RowUser RowKind = iota; RowSay; RowTool; RowDecision; RowAsk
    RowNotice; RowTodos; RowCompaction; RowReceipt; RowUsage; RowError
)

type Row struct {
    ID       string
    Kind     RowKind
    Settled  bool            // false while it may still change
    Foldable bool            // can collapse when settled
    Folded   bool
    Render   func(width int) string   // pure; termrender does the drawing
}
```

- A streaming answer is emitted incrementally by splitting at the **last blank line outside a fenced block** — the "settled prefix" rule. Everything after it stays in the live region.
- **Inline mode is a sink choice, not a second semantics.** Both modes consume the same settled/unsettled split; inline simply prints settled rows to the terminal's scrollback and keeps the live region bounded to `min(height − chrome − 1, height/2)`, while fullscreen appends them to a viewport it can repaint. Folds are marked `Foldable` at render time so inline can decide to print the *unfolded* form (it cannot repaint, so it must not print something it may later want to collapse) — one rule, applied once, instead of per-mode special cases.
- **Deltas are coalesced.** The sink drains everything already queued into a single update message, so a burst of 200 deltas costs one render rather than 200 re-parses of a growing answer. This is the primary throttle and it is not optional (G1).
- **Resize re-renders, not re-wraps.** Rows cache their rendered lines keyed by width, so a resize redraws the transcript at the new width instead of leaving stale wrapping.

#### 6.12.3 Component inventory

| Surface | Behaviour | Ticket |
| --- | --- | --- |
| Composer | Multi-line textarea; `Enter` sends, `Shift+Enter`/`Alt+Enter`/`Ctrl+J` newline; dynamic height capped at 8; themed cursor | E10-T4 |
| Completion menu | Opens on `/` or `@`; `Tab`/`Enter` accept; `@` descends one directory level per keystroke via `os.ReadDir` (never a recursive walk) and lists MCP resources alongside files; height changes only on discrete actions, never per token | E11-T2 |
| Decision card (U5) | **One** component for approvals, `ask`, workspace trust, sandbox escape and error recovery: keyboard-first verbs, consistent verbs (`y`/`a`/`p`/`n` + numbers), a preview pane, and a stable code so tests assert on codes | E11-T3 |
| Pickers | Searchable session/model/provider/theme pickers sharing one implementation; `j`/`k` navigate while the query is empty and become query text once typing starts; `Esc` cancels | E11-T4 |
| Todo panel | An 8-row window centred on the `in_progress` item, with two-level indentation and step sign-off state | E11-T5 |
| Status line (U4) | Compact default: posture + state on the left, model/effort on the right. `full`: adds cache-hit rate, context use, compaction headroom, git identity, background jobs. `off`: nothing. Toggle with `/status`; segments configurable | E11-T6 |
| Tool cards | `● Verb(arg)` with a `⎿`-style gutter, category-coloured dot, elapsed time when running, fold to a one-line summary when settled | E13-T2 |
| Diff cards | Colourised add/remove with a line-number gutter; folds computed **before** syntax highlighting (a coloured row is ~1.4 ms, so a folded preview must not pay to highlight what it discards); the external formatter runs off the render path and only invalidates the blocks that used it | E13-T3 |
| Markdown | GFM tables, fenced code with chroma, width-correct CJK emphasis, and a plain fallback if chroma is unavailable | E13-T1 |
| Image paste | `Ctrl+V` (macOS/Linux) / `Alt+V` (Windows) or `/paste-image` inserts an editable `[image #N]` attachment token; the status line shows progress until the token is ready | E11-T7 |
| Mouse & selection | Off by default over SSH; `/mouse` toggles; wheel scroll, click-to-fold, drag-select, draggable scrollbar; right-click copies a selection or pastes when there is none | E11-T8 |
| Notices | Typed, level-styled, repeat-coalesced (`×N`) | E11-T9 |
| Scrollback vs fullscreen | `--inline` prints to the terminal's own scrollback (searchable with the user's own tools); fullscreen owns the screen | E10-T6 |

#### 6.12.4 Keymap (U1)

The reference has **no central keymap registry** — dispatch is an ordered chain of hard-coded handlers, pinned by a behavioural parity test. We make the registry the source of truth:

```go
type Action string   // "composer.send", "overlay.cancel", "transcript.page_up", "rewind.open", …

type Binding struct {
    Action  Action
    Keys    []string          // defaults, e.g. {"shift+tab"}
    Context ContextKind       // "global" | "composer" | "overlay" | "approval"
    Help    string
}

// One registry drives: dispatch, /help, /keys, conflict validation at startup,
// and shell-completion-independent discoverability.
var Registry = keymap.New(
    keymap.Bind("composer.send",  "global",  "enter"),
    keymap.Bind("overlay.cancel", "global",  "esc"),
    keymap.Bind("posture.cycle",  "global",  "shift+tab"),   // ask → auto → yolo → plan
    keymap.Bind("yolo.toggle",    "global",  "ctrl+y"),
    keymap.Bind("rewind.open",    "global",  "esc esc"),
    keymap.Bind("transcript.page_down", "global", "pgdown", "ctrl+f"),
    // …
)
```

- `[agent] keymap` in config overrides defaults; a conflict or an unknown action is a startup error with the offending pair named, not a silent no-op.
- `/keys` renders the live registry (so it cannot drift from dispatch), and `/help` renders the same table grouped by context.

#### 6.12.5 Overlay ownership (U2)

```go
type Overlay interface {
    Title() string
    Update(tea.Msg) (Overlay, tea.Cmd)
    View(width int) string
    // Ownership is declared, not remembered:
    OwnsKeys() KeyOwnership   // Modal (hide composer, swallow keys) | InputOwned (composer stays live)
    ComposerVisible() bool
}
```

The layout computes bottom-region height from `ComposerVisible()` rather than trusting each panel, so "an inactive input box rendered under a modal" is structurally impossible instead of a review item.

#### 6.12.6 Theming

- A `Palette` is a fixed set of named colours (accent, muted, faint, subtle, success, warn, err, danger, info, secondary, border, selection, diff-add-bg, diff-del-bg, tool-read, tool-proc…) with **pre-resolved 256-colour fallbacks**, so a truecolor terminal and a 256-colour terminal both look intentional rather than downsampled.
- Two bases (dark, light) × a small set of accent styles per base. `/theme [auto|dark|light|<style>]` switches live and persists; `PRX_THEME` / `PRX_THEME_STYLE` override for a single run.
- `auto` resolves via an OSC 11 background probe when we own the terminal, falling back to `COLORFGBG`, falling back to dark.
- Themed pieces: composer border and cursor, transcript selection, scrollbar, notice levels, badges, diff backgrounds, status-line labels.

#### 6.12.7 Terminal hygiene

- One `cells` package owns width: grapheme-aware measurement plus deliberate `Cut`/`Hardwrap`/`Truncate` that agree with it, because "measure and cut differently" is the defect that produces lines wider than the terminal.
- Glyph substitution swaps runes the console cannot draw for stand-ins of the same counted width.
- Rows are padded to the exact content width so the scrollbar column never shifts.

### 6.13 Headless `run` and `-p`

```
prx run "fix the failing test"
prx run --auto "apply the changes"
prx -p "summarise this repo" --output-format json
echo "explain this code" | prx run
```

| Flag | Meaning |
| --- | --- |
| `--model`, `--preset`, `--effort`, `--max-steps` | Same as interactive |
| `--dir`, `--add-dir` | Workspace root; extra writable roots (runtime only, never persisted) |
| `-c/--continue`, `-r/--resume [QUERY]`, `--copy` | Session continuity |
| `--permission-mode`, `--auto`/`-y`, `--yolo` | Posture; mutually exclusive with an explicit `--permission-mode` |
| `--allowed-tools RULES` | Session-only allow rules (repeatable); configured deny still wins |
| `-p/--print` | Print only the final answer |
| `--output-format text\|json\|stream-json` | `stream-json` = one event object per line, then a final result object |
| `--events-jsonl` | Redacted lifecycle events (no prompts, reasoning, args, output) for automation that must not see content |
| `--metrics PATH` | Token/cache/cost/duration totals for a run |
| `--fail-on-unverified` | Exit `3` when declared verification did not run |

**Exit codes** (adopted because they are load-bearing for CI):

| Code | Meaning |
| --- | --- |
| `0` | The model finished — including with refused calls or unmet readiness. |
| `1` | The run failed: provider, configuration, limit or cancellation. |
| `2` | The command line was invalid. |
| `3` | `--fail-on-unverified` was given and readiness stayed unmet. |
| `4` | The folder is not trusted, so writes/commands were refused and the work was not done. |

A refusal never silently flips the exit code: refusals are listed as `permission_denials` with a stable `code` (`permission.unattended`, `permission.untrusted_folder`, `permission.read_only`, `permission.deny_rule`, `permission.declined`) and a `remedy` where one exists.

`run` and `-p` are **the same code path** as the TUI after assembly: a different `Sink` and a different approver (§6.4, G5). That is what makes the e2e suite meaningful.

### 6.14 Extensibility in v1

| Tier | v1 | Later |
| --- | --- | --- |
| **Tools** | Built-ins + `use_capability` proxy | More families (browser, LSP, best-of-n) |
| **MCP** | `stdio` + `http` transports; `tools/list`+`tools/call`; `prompts/list`+`get` as `/mcp__<server>__<prompt>`; resources as `@<server>:<uri>`; config from `[[plugins]]` and `.mcp.json`; `mcp__<server>__<tool>` namespacing | OAuth, `sse` legacy transport, registry browse/install, per-tool timeouts, `concurrency = "serial"` |
| **Skills** | Discovery + frontmatter + **inline** execution + model discovery via an index line | `runAs: subagent` execution |
| **Commands** | `.prx/commands/**/*.md`, `$ARGUMENTS`/`$1..$N`/`$$`, frontmatter `description`/`argument-hint`, subdirectory namespacing (`git/commit.md` → `/git:commit`) | MCP-prompt commands |
| **Hooks** | `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `SessionStart`, `SessionEnd`, `Stop` with regex matchers and exit-code verdicts | The full 14-event set, plugin-contributed hooks, Claude-compatible translation |
| **Read-only compat** | `.claude/commands/`, `.mcp.json`, `CLAUDE.md`/`AGENTS.md` instructions | `.agents`/`.agent` convention roots |

Two properties we keep because they are what make extensibility *safe*:

1. **MCP tools are opaque by default.** A remote tool is treated as a writer unless it declares `readOnlyHint: true`; installing a server is the trust decision, and deny rules plus the sandbox remain host-controlled boundaries.
2. **Namespacing prevents collisions** and matches the ecosystem (`mcp__<server>__<tool>`), so a server written for Claude works unchanged.

### 6.15 Instruction files and `@`-references

- **Instruction discovery** (cheap, high value, in v1): `AGENTS.md`, `CLAUDE.md`, `PRX.md`/`REASONIX.md`-style project instructions, plus `*.local.md` overrides, resolved in a documented precedence with `@path` imports. Instruction content is injected as clearly-marked, host-authored context — never merged into the cache-stable system prompt, so instruction edits do not invalidate the prompt cache mid-session.
- **Environment summary:** one small, stable startup block (OS, shell, toolchain versions) so the model stops rediscovering the machine every turn.
- **`@`-references:** `@<path>` resolves only when the path exists on disk (that existence gate is what keeps ordinary `@mention` and email addresses as literal text); a file is wrapped as `<file path="…">` (size-capped, binary files noted not dumped), a directory as a bounded recursive listing, and `@<server>:<uri>` as an MCP resource. Resolution is user-initiated, asynchronous, and **does not** pass the permission gate — it is a read the user asked for.

### 6.16 Internationalisation

A message catalog from day one (`internal/i18n`) because retrofitting it is miserable, with **English shipped** and additional catalogues out of v1 scope. Every user-facing string in `tui`, `termrender`, `run` and `cli` goes through it, including status-line labels; `[language]`/`$PRX_LANG`/`$LANG` select the catalogue. Model-visible prompt text is **not** localised by this mechanism — `[agent] reasoning_language` controls reasoning-language preference instead, which is a different concern.

---

## 7. Engineering plan

### 7.1 Testing strategy

Four levels, each with a reason to exist:

| Level | What it covers | Rule |
| --- | --- | --- |
| **Unit** | Pure logic: permission precedence and subject extraction; rule parsing/matching; todo validation; compaction planning and acceptance; compaction keep-policy budgets; config resolution (especially narrow-only project keys); diff folding; width/cut consistency; provider stream chunk folding. | No I/O, no clock, no network. Table-driven. |
| **Component** | The reducer: feed a recorded event sequence into `Transcript.Apply` and assert rows. The `Sink` boundary makes this possible without a terminal. | Assert on rows and codes, not rendered frames. |
| **Golden-ish render** | `termrender` output at fixed widths — including a 40-col and 200-col case, a CJK case, and a "console cannot draw these glyphs" case. | Assert on strings; regenerate deliberately, never blindly. |
| **End-to-end** | Assemble the real controller against a scripted fake provider, drive it headlessly, and assert events, transcript contents, exit codes, and refusal codes. A recorded session per scenario. | This is the suite that keeps the TUI and `run` from diverging (G5). |

Deliberate non-goals for tests: no golden full-screen frames (brittle), no network, no real model calls in CI. A scripted provider that replays chunk sequences is the workhorse.

**Test-tier conventions** (rules [#82]–[#90]; [`GO-STANDARDS.md` §5](./GO-STANDARDS.md#5-rule--enforcement-map-machine-decided)):

| Tier | Tag | Runs |
| --- | --- | --- |
| Unit | untagged | Every commit, with `-race`. |
| Integration | `integration` build tag | Scheduled and on demand. Touches real processes, the OS sandbox and MCP subprocesses. |
| End-to-end | `e2e` build tag | Scheduled. Assembles the real controller; still no network. |

- **No sleeping, ever** (rule [#86]), including in tests. Synchronise on channels or an injected clock; `forbidigo` fails the build on `time.Sleep`.
- **The clock is injected** (rule [#87]): timers, the spinner, flash and elapsed-time rendering read a clock interface with a fake in tests. `time.Now()` inside library code is a lint error, so untestable time usage cannot creep in.
- **Provider wire kinds are tested against `httptest`** (rule [#88]) with recorded response bodies — one fixture per kind covering text, tool calls, reasoning, usage, truncation and error.
- **Concurrency has a leak check** (rule [#62]): every package that starts a goroutine runs `goleak` in `TestMain`, turning "every goroutine has a known exit" into a test failure rather than a review opinion.
- **Benchmarks are correct or they are noise** (rule [#89]): `b.ResetTimer()` around setup, inputs recreated per iteration, `b.ReportAllocs()` where allocation matters.

### 7.2 Verification and CI

- `golangci-lint config verify` (schema-validates `.golangci.yml`) and `golangci-lint run` must exit 0. The configuration is curated and every entry cites the rule it enforces ([`.golangci.yml`](../.golangci.yml), [`GO-STANDARDS.md`](./GO-STANDARDS.md)).
- `go vet ./...` clean, and `gofmt`/`goimports` clean — the formatters run inside the same gate.
- `go test ./...` with `-race` on Linux and macOS for the **unit** tier. The `integration` and `e2e` tiers run with `-race` on a scheduled job rather than on every commit, because `-race` is expensive: this is the deliberate scheduling rule [#83] asks for, not an omission.
- **The Go practice audit is part of the definition of done**, not an optional nicety. After writing or changing Go code, the two-tier protocol in [`GO-STANDARDS.md` §4](./GO-STANDARDS.md#4-the-audit-protocol) is run and its outcome recorded on the PR: tier 1 green, the relevant §6 groups walked, and every finding either fixed or accepted with a stated reason.
- The **architecture test** (`internal/architecture/`) fails on an import edge that violates §6.2, on any import cycle, and on any package missing from its layer table.
- The **contract test** regenerates the tool-contract documentation and fails on drift (§6.6).
- The **keymap test** asserts every `Action` has a default binding, a help string and no conflicts (U1).
- The **identity test** asserts that only `internal/config/identity.go` mentions the binary/home/project-file names (§3.1) — so flipping them is provably mechanical.
- The **standards test** asserts that every linter pinned in `.golangci.yml` still exists in the running `golangci-lint` and that every rule cited in the config resolves, so a toolchain bump cannot silently drop an enforcement ([`GO-STANDARDS.md` §8](./GO-STANDARDS.md#8-keeping-this-honest)).
- `CGO_ENABLED=0` cross-compile for `darwin|linux × amd64|arm64` on every PR.

### 7.3 Observability and diagnostics

- **`prx doctor`** prints one page: version/build, OS/arch, cwd, config source paths, resolved default model, sandbox capability (backend present? bash `enforce`/`off`? write roots, network), permission posture and rule counts, MCP server health, skill/command/hook load errors, and *warnings* for ignored project settings and shadowed credentials.
- **`prx doctor capabilities`** dumps what is actually loaded (skills, commands, hooks, MCP tools) — the static view by default, `--live` to actually connect.
- **Typed codes everywhere.** Every refusal, notice and diagnostic carries a stable `code` (§6.4 rule 2). Codes are documented, tested, and safe for automation; prose is free to change.
- **`--metrics PATH`** writes tokens/cache/cost/duration so a run is measurable without reading its transcript.
- **Local logs** under `~/.prx/logs`, bounded and rotated. No telemetry upload path in v1 (§4.3).

### 7.4 Release and distribution

- `make build` → `bin/prx`; `make cross` → `dist/` for the four targets, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X …/version.Version=$(VERSION)"`.
- Version injected from `git describe --tags --always`; `prx version --json`.
- Release artifacts + `SHA256SUMS`. Signing is a follow-up, not a v1 blocker.
- A short install section (download, `go install`, and a `curl | sh` installer as a follow-up), plus shell completion (`prx completion bash|zsh|fish`).

### 7.5 Milestones

Sequenced so that **something usable exists early** and each milestone is independently demonstrable.

| M | Name | Outcome you can show someone | Epics |
| --- | --- | --- | --- |
| **M0** | **Walking skeleton** | `prx -p "read main.go and summarise it"` runs against a real model, with one provider kind, two tools, and a session on disk. | E1, E2, E3(part), E4(part), E5(part), E9(part), E14(part) |
| **M1** | **A trustworthy agent** | The full local toolbelt, permission engine, OS sandbox and workspace trust. Headless is production-credible; refusals are typed. | E5, E6, E7, E14 |
| **M2** | **The TUI** | `prx` opens a real terminal app: streaming transcript, composer, completion, decision cards, todo panel, status line, themes, resume picker. **This is the milestone that makes the project what it is.** | E10, E11, E12, E13, E9(rest) |
| **M3** | **Context and continuity** | Long sessions survive compaction; resume/continue/`--copy` are reliable; `/context` explains the window. | E8, E9 |
| **M4** | **Extensibility** | A real MCP server's tools work; a project skill and a custom slash command work; a `PreToolUse` hook can block a call. | E15, E16, E17, E18 |
| **M5** | **Ship it** | Docs, `prx setup`, `prx doctor`, completions, releases, parity matrix green. | E19, E20, E21, E22 |

### 7.6 Epic map

Detail, dependencies and per-ticket acceptance criteria are in **Appendix B**.

| Epic | Title | M | Tickets |
| --- | --- | --- | --- |
| **E1** | Project foundation, conventions and CI | M0 | 8 |
| **E2** | Configuration, paths and identity | M0 | 7 |
| **E3** | Provider layer: registry, wire kinds, streaming | M0 | 10 |
| **E4** | Turn loop, events and the controller | M0 | 9 |
| **E5** | Tool framework and the local toolbelt | M0–M1 | 14 |
| **E6** | Permission engine | M1 | 8 |
| **E7** | Sandbox and workspace trust | M1 | 7 |
| **E8** | Context management and compaction | M3 | 7 |
| **E9** | Sessions, storage and continuity | M0–M3 | 9 |
| **E10** | TUI shell, transcript and rendering | M2 | 8 |
| **E11** | TUI interaction surfaces | M2 | 10 |
| **E12** | Theming, keymap and i18n | M2 | 6 |
| **E13** | Code, diff and markdown rendering | M2 | 5 |
| **E14** | Headless `run`/`-p` and automation contract | M0–M1 | 7 |
| **E15** | MCP client | M4 | 8 |
| **E16** | Skills and custom slash commands | M4 | 7 |
| **E17** | Hooks | M4 | 5 |
| **E18** | Instructions and `@`-references | M4 | 4 |
| **E19** | CLI surface: routing, setup, doctor, completion | M5 | 6 |
| **E20** | Quality: test harness and end-to-end suite | M0–M5 | 9 |
| **E21** | Documentation and onboarding | M5 | 4 |
| **E22** | Release and distribution | M5 | 4 |

Roadmap-tier epics (not v1) are listed in Appendix B §R1.

---

## 8. Risks and mitigations

| # | Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- | --- |
| R1 | **Scope creep toward parity.** The reference is 848 K LOC; "just one more subsystem" is always available. | High | High | The tier is a decision (D1), not a default. Appendix B puts every deferred item in §R1 with a ticket stub, so deferral is explicit and reviewable rather than forgotten. |
| R2 | **Compaction quality.** A bad digest silently loses a constraint the user stated ten turns ago. | Medium | High | Budgeted user-turn retention + `[[keep]]` + never-rewrite-the-transcript + `recall`; the drop is counted and warned, never silent. Unit tests on the keep policy and acceptance math. |
| R3 | **Sandbox gaps.** A backend is missing, or a root's identity changes mid-session. | Medium | High | Enforce-without-backend refuses shell; roots are pinned; `prx doctor` reports the effective mode; the honest failure is a typed error, not a silent unconfined run. |
| R4 | **Terminal width/glyph bugs.** The classic TUI defect: lines wider than the terminal, or tofu boxes. | High | Medium | One `cells` owner for measure *and* cut; glyph substitution at counted width; golden tests at 40/80/200 columns plus a CJK case. |
| R5 | **Streaming performance.** Re-parsing a growing answer per delta is quadratic and shows up as typing lag. | Medium | Medium | Mandatory delta coalescing (one render per burst) and the settled-prefix rule; a benchmark ticket in E20. |
| R6 | **Provider wire-format drift.** Vendors change SSE shapes, reasoning fields and cache semantics. | High | Medium | One `httpclient` owns retry/watchdog; each kind is a registered factory behind the same interface; failures are typed; a recorded-fixture test per kind. |
| R7 | **Permission fatigue.** Too many prompts → the user rubber-stamps → the safety model is theatre. | Medium | Medium | Personalities are posture-based, not per-call: `auto` inside a trusted sandbox; persistent scopes shaped conservatively (command prefixes, `Edit(path)`); read-only shell commands in every mode run without a prompt. |
| R8 | **Two frontends diverge.** The TUI grows behaviour `run` does not have. | Medium | Medium | Both are `Sink`s over one controller (§6.4); the e2e suite drives the shared path; a ticket in E20 asserts the TUI adds no engine behaviour. |
| R9 | **Mirrored vocabulary drifts from upstream.** Reasonix renames a flag; our docs and its docs disagree. | Medium | Low | Mirror the vocabulary, own the semantics: every ticket states *our* behaviour. Periodic doc resync is an E21 ticket, not a live dependency. |
| R10 | **Dependency creep into a TUI project.** Charm is the point, but "just one more library" erodes the single-static-binary story. | Medium | Low | A documented exception list and a review checklist item (E1-T4): pure Go, `CGO_ENABLED=0` safe, justified in the PR. |
| R11 | **Practice drift: the baseline is documented but unenforced, so it decays into advice nobody reads.** | Medium | Medium | Both tiers are gates rather than prose. `.golangci.yml` cites the rule each linter enforces and runs on every commit; the audit is a named definition-of-done step whose findings are numbered, reasoned and recorded ([`GO-STANDARDS.md`](./GO-STANDARDS.md)). Its §8 maintenance rule binds a new rule, a linter bump and a divergence to updating the mapping **in the same commit**, so the two cannot separate. |

---

## 9. Open questions

Cheap to resolve, listed so they are not discovered late.

| # | Question | Recommendation | Cost to change |
| --- | --- | --- | --- |
| Q1 | Confirm the identity strings from §3.1 (`prx` / `~/.prx` / `prx.toml`), or use reasonix's own names for maximum doc fidelity? | Keep `prx` identity with mirrored vocabulary | One constants file + a path table; a test enforces the single-owner rule |
| Q2 | Do we ship a Homebrew tap / `curl | sh` installer in v1, or download-only? | Download-only in v1 | E22-T3 |
| Q3 | Should `--inline` be the default when stdout is not a TTY? | Yes — a piped session should write to scrollback, not try to own the screen | One flag default |
| Q4 | Do we want provider-side web search (`web_search = true`) in v1? | Not in v1; `web_fetch` covers the reading case | E15-adjacent, one provider-kind flag |
| Q5 | Should the compaction digest ever be model-editable by the user (`/compact --keep`), or is `[[keep]]` enough? | `[[keep]]` is enough for v1 | E8 scope |
| Q6 | Do we adopt the reference's `use_capability` proxy name verbatim, or rename it? | Verbatim (D4) — it is already a model-facing name | One tool name + docs |

---

## 10. Glossary

| Term | Meaning |
| --- | --- |
| **Turn** | One user message and everything the host does in response, until the model stops or a limit is hit. |
| **Step** | One model round-trip inside a turn (one request, its streamed answer, its tool batch). |
| **Batch** | The tool calls from one assistant message, partitioned into contiguous parallelisable segments. |
| **Barrier** | A tool that must stop the batch because it needs a human (`ask`, `await_user`). |
| **Posture** | The user-facing approval mode: `ask` / `auto` / `yolo` (plus `read-only`, `acceptEdits`, `dontAsk`, `plan`, `bypass` aliases). |
| **Sandbox vs permission** | Enforcement (writable roots, jail) vs policy (allow/ask/deny). Two layers on purpose. |
| **Projection** | A derived, non-canonical view of history — e.g. a compaction digest. The canonical transcript is never rewritten. |
| **Settled** | A row that will not change again and is therefore safe to print into scrollback. |
| **Sink** | The frontend interface that consumes engine events. |
| **Deferred capability** | Anything reachable only through `use_capability` rather than a schema in the prompt. |
| **Parity** | Feature-level equivalence with the reference, tracked in Appendix A's "covered by" column — never source-level equivalence. |
