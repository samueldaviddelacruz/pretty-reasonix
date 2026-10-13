# Appendix A — Reasonix reference analysis

Companion to [`TECHNICAL_DESIGN.md`](./TECHNICAL_DESIGN.md). Everything here is an observation about **`esengine/DeepSeek-Reasonix` @ `ce84fe9f`** (branch `studio`, the 2.x line), gathered read-only. The **"Covered by"** column names the pretty-reasonix epic (and milestone) that owns the equivalent — or **Deferred** with the reason.

> The reference is a moving target. Treat this as a snapshot that answers *"how did they do it, and what should we do?"* — not as a specification we are obliged to match.

---

## A1. Identity and scale

| | |
| --- | --- |
| What it is | An open-source AI coding agent: terminal, desktop, browser and editor, over one local engine. MIT. |
| Language / stack | Go 1.26 toolchain; module `reasonix`. |
| TUI stack | `charm.land/bubbletea/v2` v2.0.10, `bubbles/v2` v2.2.1, `lipgloss/v2` v2.0.6 — **already a Charm app**. |
| Notable deps | `chroma/v2` (highlighting), `goldmark` (markdown), `go-udiff`, `uniseg`, `go-runewidth`, `BurntSushi/toml`, `mvdan.cc/sh/v3` (shell parsing), `tree-sitter` (code index), `modernc.org/sqlite` (disposable catalogs), `go-keyring`, `go-gitignore`, `doublestar`, `pflag`, `go-toast`, `dbus`, `pkg/sftp`, `starlark-go` (code mode), `minisign`. |
| Size | **4,892 `.go` files / 848,551 lines.** |
| Per-package | `internal/{frontend 823, runtime 655, ext 523, contract 479, platform 391, state 335, assembly 320, session 309, tools 203, safety 194, base 173, model 125}`, `cmd 110`, `sdk 37`, `tools 65`, `desktop 15`. |
| Docs | 42 markdown files under `docs/` (SPEC 1,320 lines, GUIDE 1,932, CLI 672, …) plus `REASONIX.md` (467) and a 15 KB `reasonix.example.toml`. |
| Surfaces | TUI · `run`/`-p` headless · `serve`/`web` (HTTP+SSE) · `acp` (editors) · desktop (Studio, separate host binary) · VS Code extension. |

**Scale conclusion used for scoping:** this is not a CLI to clone line-for-line; it is a platform. D1 chose a genuine subset.

---

## A2. The architecture in one pass

| Layer | Package(s) | Owns |
| --- | --- | --- |
| Entry | `cmd/reasonix` | build info, panic capture, `cli.RunWithBuildInfo` |
| Frontends | `internal/frontend/{tui,cli,serve,acp,doctor,capdiag}` | the four surfaces + diagnostics views |
| Controller | `internal/session/control` | turn orchestration, goal/plan gates, approvals bridge, session lifecycle, behind typed sub-ports (`TurnControl`, `Approvals`, `FolderTrust`, `SessionHistory`, `Capabilities`, …) composed as `SessionAPI`/`EditorAPI` |
| Runtime | `internal/runtime/{agent,coordinator,delegation,writeclaim,completion,verdict,contract,taskcontract,plancontract,taskpolicy,recovery,guardian,usecap,capability,goaleval,bestof,isolation,taskmonitor,…}` | the model loop, evidence and verdicts, delegation and scheduling, planning, goal FSM |
| Capabilities | `internal/tools/*`, `internal/model/{openai,anthropic,responses,dashscope-responses}`, `internal/ext/*` | tool registry, provider registry, MCP + extension protocol + skills + commands + hooks |
| Contracts | `internal/contract/{provider,tool,config,permrule,planmode,plancontract,…}` | interfaces and wire types — the layer everything else depends on |
| Safety | `internal/safety/{permission,sandbox}`, `contract/permrule`, `contract/config/workspace_trust.go` | policy and enforcement |
| State | `internal/state/{store,sessionstore,sessionv4,memory,history,checkpoint,jobs,schedule,historycatalog}`, `internal/contract/config` | persistence, catalogs, checkpoints, jobs |
| Wiring | `internal/assembly/boot` | staged builder: load → extensions → providers/model → session → tools → controller |
| Platform | `internal/platform/{update,remote,forward,sftpfs,bootstrap,telemetry,crashreport,repair,account}` | updater, remote SSH, telemetry, crash reports |

**Boot order (worth copying):** load (config/roots/git) → extensions → providers/model → session → tools (registry, MCP/LSP/browser, permission policy, hooks, sub-agents) → controller. No stage reaches backwards.

**Key structural choice we decline:** the TUI is a *client of its own in-process HTTP kernel* (`serve.Hub` at `http://reasonix.local/rt/r1`, SSE on `/events`) so the terminal, desktop and browser share routes. Elegant for five surfaces; unnecessary for our two (§5.4).

---

## A3. Feature inventory

### A3.1 Core agent behaviour

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Turn loop | `Agent.Run` → `beginRunTurn` (reset turn state, freeze `TaskPolicy`, snapshot workspace) → `runToolLoop` bounded by `runMaxSteps` + grace rounds | **E4** (M0) |
| Frozen request + replay | `prepareSamplingRequest` freezes one request; up to 6 body attempts replay the *identical* payload with jittered backoff (0.5 s→8 s) | **E4-T4** (M0) |
| Stream idle watchdog | `StreamIdleTimeout = 300s`, per-provider `idle_timeout_seconds` | **E3-T4** (M0) |
| Tool batching | Cut at the first decision barrier; contiguous parallelisable runs (read-only, not sequential) with `maxParallel = 8`; writers/unknowns/sequential stay serial; one `complete_step` per round | **E4-T5** (M0) |
| Interruption | `ctx` threaded; cancelled turn writes a bounded recovery record folded into the next user turn | **E4-T6** (M0) |
| Post-turn readiness | Baseline criteria run once; `finalReadinessCheckFor`; unmet readiness returns a typed error and (in Delivery) surfaces a recovery card | **E4-T7, E14-T5** (M1) |
| Completion report | Host-authored `completion.Report` closes every turn; verdicts come from host evidence (`Evaluate`, `Obligation`) | **E4-T8** (M1) |
| Progress watch | `ProgressWatch{Rounds, TokenMultiple, Pause}` — counts idle rounds and input-token multiples; reports by default, pauses only if enabled | **E4-T9** (M1) |
| Guardians/recovery | LLM safety reviewer for tool calls; auto-recovery gate with an episode budget (3 identical failures / 6 without progress / 3 reviewer rejections → `recovery_paused`) | Deferred — v1 relies on typed errors + decision cards |

### A3.2 Context management

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Single trigger | `context_window × compact_ratio` (default 0.85). **Nothing is rewritten below it.** | **E8-T1** (M3) |
| Transaction | stable prefix + one structured digest + recent verbatim tail; acceptance ≤ 50 % of window, strictly smaller, below trigger; not padded | **E8-T2** (M3) |
| Pinned prefix | system prompt + first user turn when ≤ 1500 tokens and ≤ 15 % of window | **E8-T2** (M3) |
| Recent tail budget | `clamp(window × 10 %, 32 K, 96 K)` | **E8-T2** (M3) |
| Keep policy | active turn's opening user turn; error tool results by **record, not text**; `[keep]`-marked messages | **E8-T3** (M3) |
| User-turn retention | ≤ 1500 tokens per turn, ≤ `min(8192, window × 5 %)` total, oldest-first; `[[keep]]` pins regardless of size; drops counted (`user_kept`/`user_dropped`) and warned | **E8-T3** (M3) |
| Digest re-derivation | Every fold re-derives from the canonical transcript, never from the previous digest — no chaining, no compounding drift | **E8-T4** (M3) |
| Failure handling | Typed blocked/failed receipt per generation; no second automatic attempt; manual `compress` may retry | **E8-T5** (M3) |
| Projection vs transcript | `ProjectionMessages` keeps host records; `ModelMessages` strips `RawContent` and bookkeeping at the provider boundary | **E8-T4** (M3) |
| Tool-result bounding | Bound once on first model entry: ≤ 32 KiB visible `Content`, `RawContent` kept only when different; oversize spills to a file; `SnipHint` geometry per tool | **E5-T3** (M1) |
| `recall` / `compress` | `recall` reads `#n` positions or searches the covered region under a per-generation budget (`window × 10 %`, min 2000) | **E8-T6** (M3) |
| Memory + recall | BM25 over fact files; auto-recall before a real user turn (limit 4 / 2400 chars, project ×1.08, stale ×0.92, relative floor 0.24); `remember`/`forget`/`memory` tools; immutable ids + monotonic revisions | **Deferred** — roadmap R1 (a full subsystem in its own right) |
| History search | FTS5 contentless projection over normalized retrieval tokens; `search` + `around`; scope project/global | **Deferred** — roadmap R1 (needs a DB, which v1 avoids) |

### A3.3 Planning, goals and delegation

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Planning router | Deterministic `PlannerDecision{Route,Depth,Reason,MaxResearchRounds}`; routes `executor_only` / `plan_and_execute` / `plan_for_approval` / `plan_only`; depths `none`/`light`/`full` (research rounds 2 / 6); **never scores the task text** | Deferred — roadmap R1 |
| Two-model isolation | Planner in its own session with a read-only registry; executor in another; sessions never mix so neither prefix is disturbed | Deferred — roadmap R1 |
| Plan mode FSM | `planmode.Runtime`: `Inactive/Planning/AwaitingApproval/Executing`; widening transitions bump an epoch; stale authority is refused | **Partial in v1:** `--permission-mode plan` is a posture, the FSM is roadmap |
| Approved-plan window | After approval, writers auto-approve **for that execution turn only**; future plans still wait | Deferred — roadmap R1 |
| Goal mode | `/goal <objective>` as an autonomous session-scoped task contract; continuation loop until complete, a repeated blocked state (3×, normalized), a user stop, or a limit | Deferred — roadmap R1 |
| Delegation | `task` / `read_only_task` / `parallel_tasks` / `fleet` / `read_subagent_result` / `list_subagents`; profiles from `runAs: subagent` skills | Deferred — roadmap R1 (see rationale below) |
| Write claims | A writer always claims a path set; omitting it claims the whole workspace and serialises against every other writer; paths verified with symlink resolution and both ends of a move | Deferred — roadmap R1 |
| Scheduler | 6 concurrent sub-agents (cap 32), 3 parallel writers, nested acquires fail fast to avoid parent/child deadlock | Deferred — roadmap R1 |
| Completion adjudication | `complete_subtask` submits a *claim*; the host checks citations against receipts, lowers unbacked criteria, and never raises a status | **Partial in v1:** the same discipline for `complete_step` (**E4-T8, E5-T13**) |
| Context capsule | Per child run: workspace, prompt source + hash, tool scope + schema hash, model, effort, parent ids, an `inherited` block, and a stable `capsuleHash` | Deferred — roadmap R1 |

> **Why delegation is deferred, in the reference's own numbers:** it measured forced delegation at **2.4–4.5× the tokens and 3.7–4.1× the wall time** for identical outcomes across four task shapes, with 100 % success in both arms; the migration case was *worse* split up (one child read a file, wrote a script and fixed 108 call sites in 28 s). Its own conclusion is that a brake is the correct default. Reimplementing a 655-file scheduler to get a token *regression* is not a good v1 trade.

### A3.4 Todos, evidence and contracts

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| `todo_write` | Read-only but sequential; replaces the whole list; two levels; exactly one `in_progress`; `step_id` copied verbatim; rejects removing/regressing the in-progress item, breaking the completed prefix, or completing an item without a `complete_step` receipt; re-sending the current list is acknowledged as unchanged | **E5-T12** (M1) |
| `complete_step` | One step per call; ≥ 1 evidence item of `verification`/`review`/`diff`/`files`/`manual`; host verifies cited commands have successful receipts and cited paths have write/read receipts; `manual` cannot stand alone once receipts exist; refused in plan mode; host advances the list and emits a synthetic `todo_write` | **E5-T13** (M1) |
| Task contract | Context / Request / Output format / Constraints / Pause policy; frozen per turn (`taskpolicy.Derive`), injected as an `<execution-policy>` block on the user turn | **E4-T3** (M0, partial) |
| Pause policy | Pause only for irreversible or externally visible work, scope changes, or information only the user has; uncertainty uses a default and is reported as an assumption | **E4-T3, E21-T2** (docs) |

### A3.5 Permissions and sandbox

Detail in §A6 below. Summary of what exists: postures `read-only`/`manual`/`ask`/`auto`/`acceptEdits`/`dontAsk`/`plan`/`bypassPermissions` (+ 1.x aliases resolving to Auto and Yolo); rule grammar `Tool`, `Tool(specifier)`, legacy `Tool=literal`; precedence `deny > sessionAllow > ask > allow > read-allowed > human-required > fallback`; approver scopes once/session/always; dynamic-bash strictness; recursive-delete guard; workspace trust; seatbelt/bubblewrap jails with pinned write roots; sandbox escape behind a fresh human approval.

**Covered by:** **E6** (M1) and **E7** (M1) — adopted essentially in full, minus browser/computer rule families (their tools are deferred) and minus the escape path (roadmap).

### A3.6 Tools

Full model-facing inventory. **R** = read-only. The last column is our v1 decision.

| Tool | Purpose | R | v1 |
| --- | --- | --- | --- |
| `read_file` | Line-numbered read, offset/limit | ✓ | **E5-T4** |
| `write_file` | Whole-file write, creates parents | | **E5-T5** |
| `edit_file` | Replace one unique string | | **E5-T6** |
| `multi_edit` | Atomic ordered edit list | | **E5-T6** |
| `move_file` | Move/rename, both ends root-checked | | **E5-T7** |
| `delete_range` | Delete a line span by unique anchors; returns a diff | | **E5-T7** |
| `delete_symbol` | Delete a Go symbol via AST | | Deferred (needs tree-sitter/AST machinery) |
| `notebook_edit` | Edit one `.ipynb` cell | | Deferred |
| `ls` | Directory listing (recursive optional) | ✓ | **E5-T4** |
| `glob` | Path globbing, bounded results + timeout | ✓ | **E5-T4** |
| `grep` | Regex search, bounded + timeout | ✓ | **E5-T4** |
| `code_index` | Local symbol outline/definition fallback | ✓ | Deferred |
| `bash` | Shell, combined output, background mode, sandboxed | | **E5-T8** |
| `bash_output` | Read new output of a background job | ✓ | **E5-T9** |
| `kill_shell` | Terminate a background job | | **E5-T9** |
| `wait` | Block until background jobs finish | ✓ | **E5-T9** |
| `todo_write` | Replace the task list | ✓ | **E5-T12** |
| `complete_step` | Sign off one step with host-verified evidence | ✓ | **E5-T13** |
| `update_goal` | Per-turn goal disposition | ✓ | Deferred (goal mode) |
| `compress` | Summarise part of the conversation | ✓ | **E8-T6** |
| `recall` | Re-read folded `#n` or search the covered region | ✓ | **E8-T6** |
| `context_budget` | Report remaining context room | ✓ | **E8-T7** |
| `render_chart` | Inline chart from a small spec | ✓ | Deferred |
| `web_fetch` | URL → text, SSRF-guarded | ✓ | **E5-T10** |
| `web_search` | Search → summary + sources | ✓ | Deferred (provider-side feature) |
| `ask` | Ask 1–4 questions with options | ✓ | **E5-T11** |
| `await_user` | Hand the turn back with the list open | ✓ | **E5-T11** |
| `conclude_blocked` | Declare the work impossible | ✓ | Deferred (not on the lean surface upstream either) |
| `submit_plan` | Structured plan submission | ✓ | Deferred (plan FSM) |
| `conclude_no_changes` | End a planning turn with no plan | ✓ | Deferred |
| `review_report` | Structured review verdict | ✓ | Deferred |
| `complete_subtask` | Close a delegated task with a claim | ✓ | Deferred |
| `task` | Spawn a sub-agent | | Deferred (delegation) |
| `read_only_task` | Strict read-only sub-agent | ✓ | Deferred |
| `parallel_tasks` | N read-only sub-agents | ✓ | Deferred |
| `fleet` | 2–64 sub-agents as a dependency DAG | | Deferred |
| `read_subagent_result` | Page a child's full answer | ✓ | Deferred |
| `list_subagents` | Enumerate children with refs | ✓ | Deferred |
| `use_capability` | Search/inspect/call deferred capabilities | ✓ | **E15-T5** (the MCP proxy) |
| `apply_isolated` / `discard_isolated` | Write/discard a worktree-isolated task | | Deferred |
| `run_skill` / `read_only_skill` / `read_skill` | Invoke / load skills | mixed | **E16-T3** (`read_skill`, `run_skill` inline; read-only-subagent variant deferred) |
| `install_skill` | Write a skill file | | Deferred to the UI-driven flow (**E16-T5**) |
| `slash_command` | Invoke a project slash command | ✓ | **E16-T4** |
| `list_sessions` / `read_session` | Inspect saved sessions | ✓ | Deferred (roadmap; the picker covers the human case) |
| `history` | BM25 search across session history | ✓ | Deferred |
| `memory` / `remember` / `forget` | Memory facts | mixed | Deferred |
| `docs` | Search the bundled docs corpus | ✓ | Deferred |
| `install_source` | Plan/install/uninstall a skill/MCP/plugin | | Deferred |
| `run_script` | Starlark "code mode" calling tools | | Deferred |
| `advise` | Ask a stronger model | ✓ | Deferred |
| `best_of_n` | N isolated attempts + a judge | | Deferred |
| `system_one` | Typed-decision backend | ✓ | Deferred |
| `browser_open` / `browser_read` / `browser_act` | Drive a browser | mixed | Deferred |
| `computer_read` / `computer_act` | See/operate another app | | Deferred |
| `lsp_definition` / `_references` / `_hover` / `_diagnostics` | LSP queries | ✓ | Deferred |
| `mcp__<server>__<tool>` | MCP tool calls | varies | **E15-T4** |

**Contract conventions worth copying** (all adopted, ticket **E5-T2**):
- The `Tool` interface is minimal (`Name`/`Description`/`Schema`/`Execute`); optional behaviour is discovered by **type assertion** (`ReadOnly`, `Sequential`, `PlanSafe`, `SnipHinter`, `Paged`, `Preflighter`, `DecisionBarrier`, `PathWriter`, `Paged`), never by name lookup.
- Schemas are inline JSON literals, canonicalised once at registry insert and cached; a generated tool-contract document is asserted against that same path by a test.
- `Execute` parses its own args; errors are returned as data so the model self-corrects.
- **Read-only ≠ plan-safe.** `complete_step` is read-only but *not* plan-safe; `read_only_task` is both. Two independent properties.

### A3.7 Extensibility

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| MCP transports | `stdio` (NDJSON over a subprocess), `http`/`streamable-http` (`Mcp-Session-Id` echoed, same-server calls serialised), legacy `sse` | **E15-T2** (stdio + http); `sse` deferred |
| MCP lifecycle | `initialize` → `notifications/initialized` → `tools/list`; startup concurrency 8, startup timeout 5 s (configurable 30 s), call timeout 300 s; `protocolVersion = 2025-11-25` | **E15-T2** |
| MCP auth | Protected-resource + AS metadata discovery, DCR, PKCE S256, loopback callback, refresh rotation; tokens bound to a resource URL | Deferred — roadmap R1 |
| MCP config sources | `[[plugins]]` in user/project config; project `.mcp.json` (Claude schema) merged after TOML, TOML wins on name clash; legacy sources | **E15-T6** |
| MCP namespacing | `mcp__<server>__<tool>` (spaces normalised, hash salt on collision) to match Claude Code | **E15-T4** |
| MCP prompts/resources | `prompts/list`+`get` → `/mcp__<server>__<prompt>`; `resources/list`+`read` → `@<server>:<uri>`; `/mcp` shows servers and counts | **E15-T3** |
| Read-only hints | `annotations.readOnlyHint` → `ReadOnly()`; defaults to **false** (a remote tool is opaque); `destructiveHint` stays an internal fact | **E15-T4** |
| Per-server concurrency | `concurrency = "parallel"` (default) or `"serial"`; known stateful names (browser/playwright/puppeteer/chrome/chromium/selenium) default to serial | Deferred (single-agent v1) |
| Extension Protocol v2 | Sidecar NDJSON JSON-RPC (`reasonix.extension.v2`); a *subset* of the manifest must be declared at initialize; 17 interception points (`input.receive`, `agent.before_start`, `system_prompt.build`, `context.prepare`, `provider.request/response`, `tool.before/after`, `permission.decision`, `compaction.prepare/complete`, `frontend.event`, …); slots with single owners; contributes tools (`ext__…`), providers, UI surfaces; **full trust** (runs outside the sandbox; a `permission.decision` allow can override a host deny) | Deferred — roadmap R1 |
| Skills | `<name>/SKILL.md` or flat `<name>.md`; roots: project → custom `[skills] paths` → global → home conventions (`.reasonix`, `.agents`, `.agent`, `.claude`); frontmatter keys `name, description, runas, context, agent, allowed-tools, model, effort, read-only, triggers, auto-use, cost, invocation, requires, disable-model-invocation, user-invocable, argument-hint, paths`; `runAs: inline` joins the turn, `runAs: subagent` runs an isolated child | **E16-T1..T3** (inline); `runAs: subagent` deferred |
| Skill index | Only a compact index line is pinned so the prompt stays cache-stable; `manually`-invoked profiles are excluded from the index | **E16-T2** |
| Custom slash commands | Markdown templates; name from path (subdirs → `:`); frontmatter `description`, `argument-hint`; `$ARGUMENTS`, `$1..$N`, `$$`; later dir wins on clash | **E16-T4** |
| Hooks | 14 events (`PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, `UserPromptSubmit`, `Stop`, `StopFailure`, `PostLLMCall`, `SessionStart`, `SessionEnd`, `SubagentStart`, `SubagentStop`, `Notification`, `PreCompact`); blocking only `PreToolUse`/`UserPromptSubmit`; anchored-regex matchers over tool names; JSON payload on stdin; exit 0 pass / 2 block (blocking events) / other warn; spawn error or timeout on a blocking event blocks; project hooks need approval before they run | **E17-T1..T5** (6 events) |
| ACP | Agent Client Protocol v1 over NDJSON stdio; `initialize` advertises sessions, prompt image + embedded context, `mcpCapabilities`; `session/new|load|resume|prompt|cancel|list|close|delete`; controls for model/effort/preset/tool-approval; vendor `_reasonix.io/session/steer` + a durable inbox | Deferred — roadmap R1 |
| Themes | CLI theming is `termrender.Palette` only; the `theme.json`/CSS pack format in `docs/THEME_PACK.md` belongs to the **retired Wails desktop / current Studio**, *not* the terminal | **E12** (our own palette, no pack format) |
| Plugin packages | `plugin-packages.json`, manifest v1/v2, Claude/Codex compatibility, `prx plugin install/list/show/…` | Deferred — roadmap R1 |
| Community market | Registry browse/install (never a trust root) | Deferred — roadmap R1 |

### A3.8 Instructions, environment and references

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Instruction files | `REASONIX.md`, `AGENTS.md`, `CLAUDE.md`, `*.local.md`, with `@path` imports and a resolved precedence | **E18-T1** |
| Environment summary | A small stable startup block (OS, shell, common tool versions), with optional explicit trusted tool paths | **E18-T2** |
| `@`-references | `@<server>:<uri>` → MCP resource; `@<path>` → local file/dir, **only when the path exists** (the existence gate keeps `@mention` and emails literal); files size-capped, directories as bounded listings; async; bypasses the permission gate because the user asked | **E18-T3** |
| Syntax/symbol index | tree-sitter based `code_index` | Deferred |
| Language preference | `reasoning_language = auto|zh|en` for *visible reasoning*; separate from UI language | **E12-T5** |

### A3.9 Modes and presets

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Collaboration modes | `normal` / `plan` / `goal` — separate from tool approval, entered only by explicit user choice | `plan` as a posture in v1 (**E6-T8**); `goal` deferred |
| Execution presets | `balanced` (default) / `delivery` (legacy `economy`/`light` → `balanced`); change host policy, not the tool list; all presets share the same provider-visible surface | **E4-T9** (partial: preset plumbing + Delivery readiness), rest in roadmap |
| Reasoning effort | Per-provider effort levels, overridable per session/sub-agent/profile | **E3-T9, E19-adjacent** |
| Output styles | `/output-style` selects an answer persona | **E12-T6** |
| Permission postures | As §A6. Shift+Tab cycles read-only → ask → auto → YOLO → plan; Ctrl+Y toggles YOLO and returns to the previous posture | **E11-T3** |
| Verbose/expanded reasoning | `/verbose` toggles expanded reasoning display | **E10-T7** |
| Budgets | No round/token/time/cost limit by default; opt-in `task_cost_budget`, `task_time_budget_minutes`, `goal_token_budget` | Deferred except `--max-steps` (**E14-T3**) |

### A3.10 Sessions, storage and recovery

| Feature | Reference behaviour | Covered by |
| --- | --- | --- |
| Event log | `<id>.events.jsonl` authoritative (mode `0600`), two record kinds `append`/`replace`, chained `message_index`, `content_digest`, `writer_id`, fsynced | **E9-T2** (M0) |
| Compatibility transcript | `<id>.jsonl` written alongside; the reader prefers the event log | **E9-T2** |
| Tolerant replay | Stop at the first unparseable/non-chaining record, return the valid prefix, truncate to the last good boundary, salvage the rest to `.damaged`; unknown schema/type is a **hard** error; size/count preflight (512 MiB, 100 k records) | **E9-T3** (M0) |
| Log folding | Fold to a single `replace` at 128 MiB or 4× live content (floor 256 KiB) via temp+rename | **E9-T3** |
| Sidecars | ~20 files: index, display-index, meta, recovery, context, goal-state, execution/adjudication/conflicts, wire, leases, salvage, `ckpt/`, `outputs/`, `blobs/`, `jobs/`, `inbox/` | **E9-T4** (index only in v1) |
| Lifecycle | `new` / `resume` / `continue` / `copy` / `fork` / `branch` / `branch-at` / `switch`, with `BranchMeta{ParentID}` | **E9-T5** (new/resume/continue/copy); branching deferred |
| Picker index | Reads the meta sidecar's turn count + preview **without decoding the transcript**; unknown counts show "History is being indexed" and are repaired in the background | **E9-T6** |
| Single writer | Lease file + lease metadata (`writer_id, pid, hostname, acquired_at`); a generation-bound in-memory write authority fails closed; `Release` drains active saves to avoid an ABA handoff | **E9-T7** (simplified: lease + typed refusal) |
| Crash repair | `InFlightTurn` meta (start index/revision/digest, commit digest); resume truncates only when the commit digest is absent; genuine stale-runtime conflicts fork a bounded recovery branch (depth ≤ 3) | **E9-T8** (truncate/keep only) |
| 1.x read-only format | `sessions-v4` with zstd frames and SHA-256 verification, read-only | Not supported (D2: no foreign formats) |
| Checkpoints / rewind | Git-free per-turn file snapshots; `<id>.ckpt/turn-<N>.json` + content-addressed blobs; retain 100 checkpoints / 1 GiB quota / 32 MiB max file; durable restore transaction (`prepared → committing → committed/aborted/undone`) with compensating undo; reports **coverage** gaps (`bash_side_effect`, `hook_write`, `mcp_external`, `outside_workspace`, `symlink`, `hardlink`, `oversized`, `background_writer_cross_turn`) and **conflicts** (`manual_edit`, `external_change`, `deleted_and_recreated`, `type_change`, `missing_payload`, `active_writer`, `stale_plan`); never touches `.git` | Deferred — roadmap R1 (adopting the coverage/conflict reporting discipline when we do) |
| Jobs and artifacts | Session-scoped background jobs; `<id>.jobs/<jobID>.log` + `.json` metadata via atomic rename; `Done/Failed/Killed/Interrupted` | **E5-T9** (in-memory + output) |
| Task monitor / schedules | Observation layer over jobs; durable `schedules.json` with at/every triggers and grants | Deferred |
| Durable inbox / steering | Queued follow-ups, editable/movable/pausable | Deferred (v1: `ask` + interrupt) |

### A3.11 Platform services

| Service | Reference behaviour | Covered by |
| --- | --- | --- |
| Updater | minisign-verified GitHub release selection (strict `vX.Y.Z`, non-prerelease), SHA256SUMS, in-place update | **E22** (checksums + manual); auto-update deferred |
| Credentials | Provider keys in `<home>/.env` (`0600`) as the only runtime source; keyring is a *migration* source; tombstones `# reasonix-cleared KEY` | **E2-T5** (own store, no keyring) |
| Serve/web | HTTP+SSE, `auth_mode token|password|none`, launch token in a `0600` file, QR code, loopback-only credential writing | Deferred — roadmap R1 |
| Remote SSH | Bootstrap a detached remote `serve`, tunnel, provider broker so the remote needs no key, SFTP file ops, port forwards, host-key TOFU | Deferred — roadmap R1 |
| ACP | See A3.7 | Deferred |
| Telemetry | Opt-in, once-per-day install ping + bucketed counters; opt-out via `DO_NOT_TRACK`/`REASONIX_TELEMETRY`/CI; never uploads prompts, paths, ids, model names or exact values | **Not adopted** (no upload path in v1) |
| Crash reports | Sanitized local reports (max 10, owner-only), never auto-uploaded, `report list/show/send/delete` | **E19-T5** (local capture only, no send) |
| Doctor | Default report + `capabilities`, `quality`, `session`, `redact-sessions`, `runtime`, `billing`, `repair`, `catalogs` subcommands | **E19-T3** (`doctor`, `doctor capabilities`, `doctor session`) |
| Config repair | Quarantine an invalid config and restore a last-known-good snapshot | **E2-T7** (warnings + quarantine) |

---

## A4. CLI surface

### A4.1 Subcommands

| Verb | Aliases | Purpose | v1 |
| --- | --- | --- | --- |
| `run` | — | Headless task | **E14** |
| `tui` | `chat`, `code` | Terminal UI (bare invocation) | **E10** |
| `setup` | — | Staged provider/model manager | **E19-T2** |
| `config` | — | `reasoning-language`, `compact-ratio`, `currency`, `telemetry` | **E19-T4** (subset) |
| `init` | — | Create project instructions | **E18-T1** |
| `mcp` | — | `add,get,remove,list,enable,disable,retry,trust,auth,update,import,browse,install` | **E15-T7** (`add/list/remove/enable/disable/retry`) |
| `doctor` | — | Diagnostics + subcommands | **E19-T3** |
| `report` | — | Crash reports | **E19-T5** (local only) |
| `session`/`sessions`/`catalogs` | — | Machine session state, reindexing | Deferred (no redacted machine API in v1) |
| `hook`/`hooks` | — | Hook state | **E17-T5** |
| `trust` | — | Approve/revoke a folder | **E7-T6** |
| `subagent` | — | Sub-agent profiles | Deferred |
| `review` | — | AI code review | Deferred |
| `plugin` | — | Plugin packages | Deferred |
| `remote` | — | Remote SSH | Deferred |
| `serve` / `web` | — | HTTP frontend | Deferred |
| `acp` | — | Editor protocol | Deferred |
| `upgrade`/`update` | — | Self-update | Deferred (document the manual path) |
| `version` | `-v` | Version/build info (`--verbose --json`) | **E19-T6** |
| `completion` | — | Shell completion | **E19-T6** |
| `login`/`whoami`/`logout` | — | Account | Not applicable |
| `task` | — | Task control/monitoring | Deferred |
| `docs-manifest` | — | Docs manifest | Not applicable |

### A4.2 Global flags and exit codes

- **Flag placement:** the reference has no separate global parser — a leading `-p/--print` reroutes to `run --print`, and leading session flags are unioned across the TUI and `run` flag sets, allowed before the verb *only* when both read them identically. Flags may sit anywhere among a `run` task's words.
- **Exit codes:** `0` finished (including with refusals or unmet readiness) · `1` run failure · `2` usage · `3` `--fail-on-unverified` unmet · `4` untrusted folder refused.
- **Refusal codes:** `permission.unattended`, `permission.untrusted_folder`, `permission.read_only`, `permission.deny_rule`, `permission.declined`, plus sandbox/shell codes (`shell.delete_sequence`, `shell.delete_nonliteral`, `shell.destructive_target`, `shell.delete_option`, `shell.syntax_error`, `shell.parser_unavailable`, `shell.analysis_unknown`, `shell.command_line_too_long`) and recovery/budget codes (`recovery_paused`, `budget_spend`).

**Covered by: E14-T1..T7 and E19-T1.**

### A4.3 In-session slash commands (reference vocabulary)

`/help` (generated from the dispatch registry) · `/model` · `/provider` · `/resume` · `/status` · `/preset` (`/work-mode`, `/profile`) · `/theme` · `/effort` · `/output-style` · `/verbose` · `/sandbox` · `/mcp` · `/skills` · `/hooks` · `/memory` · `/rewind` · `/tree` · `/branch` · `/switch` · `/reload` · `/new` · `/clear` · `/compact` · `/context` · `/todo` · `/goal` · `/docs` · `/remember` · `/currency` · `/language` · `/reasoning-language` · `/mouse` · `/paste-image` · `/queue` · `/steer` · `/cls` · `/quit`

**v1 subset (E11-T10):** `/help`, `/model`, `/provider`, `/resume`, `/status`, `/preset`, `/theme`, `/effort`, `/output-style`, `/verbose`, `/sandbox`, `/mcp`, `/skills`, `/hooks`, `/new`, `/clear`, `/compact`, `/context`, `/todo`, `/keys`, `/mouse`, `/paste-image`, `/quit` + dynamic `/mcp__…` prompt commands and project commands.

---

## A5. On-disk formats (reference, for contrast — we define our own)

| Path | Purpose |
| --- | --- |
| `~/.reasonix/config.toml` | global config |
| `~/.reasonix/.env` | provider secrets (`0600`), the only runtime credential source |
| `~/.reasonix/{commands,skills}/`, `settings.json`, `remote/known_hosts` | commands, skills, hooks, SSH |
| `<state>/sessions/`, `archive/`, `memory/global/`, `projects/<slug>/memory/`, `stats/` | session and memory artifacts |
| `<cache>/{session-catalog/v3,history-search/v1,usage-catalog/v1,task-catalog/v1}.sqlite` | disposable projections |
| `<id>.jsonl`, `<id>.events.jsonl`, `<id>.event-index.json`, `<id>.display-index.json`, `<id>.meta`, `.recovery.json`, `.context.json`, `.goal-state.json`, `.execution.jsonl`, `.adjudication.jsonl`, `.conflicts.jsonl`, `.wire.jsonl`, `.lock`, `.lease.lock`, `.lease.json`, `.ckpt/`, `.outputs/`, `.blobs/`, `.jobs/`, `.inbox/` | the session file family |
| `./reasonix.toml`, `./.mcp.json`, `./.reasonix/settings.json`, `./.reasonix/commands/`, `./.reasonix/skills/<name>/SKILL.md` | project-scoped |

**Our version** is in `TECHNICAL_DESIGN.md` §6.10/§6.11 — deliberately much smaller: config, `.env`, `trust.json`, `commands/`, `skills/`, `settings.json`, `sessions/<id>.{jsonl,events.jsonl,index.json,lock,outputs/}`, `logs/`.

---

## A6. Permissions and sandbox in detail

**Rule grammar** (`Tool` | `Tool(specifier)` | `Tool=literal`), **subject extraction** from args by a fixed key set (`planId, command, file_path, path, source_path, destination_path, pattern, origin, app`), **families** `Bash` / `Edit` (file mutation) / `Browser` / `Computer`.

**Per-segment bash evaluation:** `:*` suffix = command-prefix approval; a prefix rule also rejects later commands that introduce shell operators, so `Bash(go test:*)` does **not** cover `go test ./... && rm -rf tmp`; legacy `Bash(go test *)` still loads but new rules are saved as `Bash(go test:*)`; subject extraction peels wrappers (`env`, `timeout`, `nice`, …) via a shell parser.

**Memory scopes for learned rules:** bash → safe command prefix or an exact literal; file mutations → a shared `Edit(<path>)`; browser/computer → keyed on origin/app.

**Sandbox spec:** `Mode: "enforce"`, write roots + pinned identity, read roots, `ForbidReadRoots`, `Network`, host authorities (`ssh_agent`/`docker`/`podman`), egress allow/deny with closed loopback ports, minimal writes, shell + shell env, session temp. macOS: `sandbox-exec -p <SBPL>` with `(allow default)(deny file-write*)` + allowed dirs, per-forbid-root `deny file-read*` **and** `file-write*`, `(deny network*)` unless enabled. Linux: bubblewrap `--unshare-net --ro-bind / /` + tmpfs/bind mounts and `--bind` write roots; forbid-read via read-only `/dev/null` binds or tmpfs. Windows: no backend — mode pinned `off`, bash unconfined.

**Escape path:** a denied or unavailable-sandbox failure can be rerun unconfined behind a fresh human approval. **We defer this** and keep the honest refusal instead; it is recorded in roadmap R1.

---

## A7. Notable UX decisions documented by the reference

Worth reading as design intent, whether or not we copy the implementation.

1. **Cache stability beats convenience.** The only automatic history rewrite is the compaction trigger; the reference explicitly says *why* it never rewrites history below the trigger ("any rewrite would invalidate the prompt cache from that point on") — and why switching models inside one conversation is forbidden (it breaks the prefix).
2. **Fail closed, then explain.** Headless `ask` refuses writes and *lists* them; an untrusted folder exits `4` rather than looking like success; `deny` is a hard block in every posture.
3. **Prompts are rare and deliberate.** Telemetry consent is asked once ever; the trust-folder prompt appears only when a recorded decision is missing and the sandbox confines writes.
4. **Progressive disclosure everywhere.** `use_capability` for optional tool schemas; static diagnostics by default with `--live` opt-in; `/docs` searches locally before spending a model call; separate `doctor` levels.
5. **Structural judgements, not textual ones.** Permission subjects come from parsed commands; `KeepErrors` classifies a failure from the host's execution record rather than from log text (because a real `go test` log opens with `=== RUN` and no prefix match can see it).
6. **Policy ≠ enforcement.** Permissions decide; the sandbox enforces. `ReadOnly` is an integrity property and makes no confidentiality or authority claim.
7. **The host never inflates a status.** Sub-agent completion is a claim adjudicated against receipts; criteria that cannot be backed are lowered; the host advances todos itself.
8. **Silence can be a bug.** Compaction telemetry counts dropped user turns, and a checkpoint that folded one emits a warning naming the `[[keep]]` escape hatch — because a projection reads as complete either way.
9. **Determinism where it is cheap.** The planning router reads pristine user text plus trusted turn metadata and never calls a classifier model.
10. **Measure before believing.** The delegation instrumentation (`subagent_runs`, `false_completions`, `write_scope_violations`, `duplicate_work_paths`) exists because orchestration looks like progress while costing more; the reference documents a measured 19 % median per-task token variance between identical runs.

---

## A8. Where the reference's UX falls short (our opportunity list)

These are the observations behind the UX thesis (TDD §4.2).

| # | Observation | Opportunity |
| --- | --- | --- |
| O1 | **No central keymap registry.** Dispatch is an ordered chain of handlers (`screenKey` → `shortcutKey` → `menuKey` → `viKey` → composer), pinned by a ~30-test behavioural parity suite. Keys are not rebindable. | One registry drives dispatch, `/help` and `/keys`, with config overrides and startup conflict detection. (**U1, E12-T3**) |
| O2 | **Overlay ownership is a convention.** A new modal must remember to hide the composer and extend layout tests. | Declare ownership in the overlay interface; derive composer visibility. (**U2, E10-T5**) |
| O3 | **Inline and fullscreen behave differently.** Inline cannot repaint, so folds become pinned/substituted and diff formatting runs synchronously; the renderer branches on `scr != nil` in several places. | One settled/unsettled row model; mode changes only the sink. (**U3, E10-T6**) |
| O4 | **The footer is dense and fixed.** Posture + state left, model/effort right, and a second row of telemetry. Narrow terminals compact groups rather than cutting labels. | Compact default + toggleable `full`, configurable segments. (**U4, E11-T6**) |
| O5 | **Interruptions come in several flavours.** Approvals, `ask` cards, URL-origin asks, setup, connection and recovery cards are related but separate surface types. | One decision-card component with consistent verbs and a stable code. (**U5, E11-T3**) |
| O6 | **Context and cost are telemetry, not a view.** Cache hit rate, context use and compaction headroom live in the footer; there is no first-class "what is filling my window" breakdown. | `/context` breakdown + `/status` drawer. (**U6, E8-T7, E11-T6**) |
| O7 | **~60 model-facing tools.** Kept manageable only by the capability proxy and phase visibility. | Same proxy, a much smaller always-on surface. (**U7, E5-T14**) |
| O8 | **Rewind coverage is subtle.** Correct and honest, but the user must understand `bash_side_effect` vs `manual_edit` to trust it. | When we build it, surface coverage in the picker row itself, not just in the result. (roadmap) |
| O9 | **Session choice is thin.** The picker reads an index sidecar for counts and preview; rich metadata (model, first prompt, turn count, duration) is not the organising idea. | Preview-rich picker. (**U10, E9-T6**) |
| O10 | **Discovery is generated but not joined up.** `/help` comes from the registry, but keybindings, config keys and diagnostics live in three places. | `/help`, `/keys`, `/status` and `--help` from one registry each, plus `prx doctor` cross-references. (**U9, E19-T3**) |
| O11 | **A lot of surface for one user's daily work.** Four frontends, a desktop app, remote SSH, an extension protocol and a plugin marketplace all exist; the *core loop* has to be discovered underneath them. | Two surfaces, one loop, everything else in a labelled roadmap tier. (**D1/D3**) |
| O12 | **Windows ships without the safety story.** Bash runs unconfined and the config that said `enforce` silently resolves to `off`. | Scope v1 to the platforms where the sandbox is real; add Windows with an explicit, documented posture. (**§4.3, E7-T5**) |

---

## A9. Reference documentation index

Useful when a ticket needs upstream context. All under `/tmp/deepseek-reasonix` in this analysis, or the `docs/` directory of the upstream repository.

| File | Lines | Use for |
| --- | --- | --- |
| `README.md` | 318 | positioning, surfaces, install, version lines |
| `docs/SPEC.md` | 1,320 | the engineering contract: abstractions, config, §3.3 MCP, §3.6 context, §3.7 permissions, §3.10–3.16 delegation |
| `docs/GUIDE.md` | 1,932 | day-to-day config and usage; the feature bible |
| `docs/CLI.md` | 672 | flags, postures, exit codes, output formats, slash commands |
| `REASONIX.md` | 467 | in-repo agent instructions; instruction-precedence behaviour |
| `docs/TOOL_CONTRACT.md` | 132 | the documented tool surface (generated and asserted) |
| `docs/TOOL_APPROVAL_MODES.md` | 152 | posture semantics and the dynamic-bash rules |
| `docs/TASK_CONTRACT.md` | 87 | the contract/pause-policy template |
| `docs/CHECKPOINTS.md` | 144 | snapshot model, coverage and conflict codes |
| `docs/SESSION_MEMORY_RETRIEVAL.md` | 337 | memory, recall and Context Engine v2 |
| `docs/SKILLS.md`, `docs/SUBAGENT_PROFILES.md`, `docs/EXTENSIONS.md`, `docs/EXTENSION_PROTOCOL.md`, `docs/PLUGIN_PACKAGES.md` | 255–731 | the extensibility tiers |
| `docs/CONFIG_PATHS.md` | 361 | every path, migration rules, credential rules |
| `docs/ACP.md` | 357 | editor integration |
| `docs/CAPABILITY_DIAGNOSTICS.md` | 266 | `doctor capabilities` and the diagnostics schema |
| `docs/ROADMAP.md`, `docs/MIGRATING.md`, `docs/RECOVERY.md` | 92–124 | what upstream itself defers, and the 1.x→2.x split |
| `reasonix.example.toml` | 15 KB | the fully-annotated config, field by field |

---

## A10. What we deliberately ignore, and why

| Reference thing | Ignored because |
| --- | --- |
| In-process HTTP kernel as the frontend transport | Two surfaces, not five; a socket adds latency and test surface for no parity gain (D3). |
| 1.x `sessions-v4` read-only format | D2: our own formats. Supporting a foreign compressed frame format is pure cost in v1. |
| SQLite catalogs (session/history/usage/task) | All disposable projections for search and accounting features that are out of v1. No database in v1 at all. |
| Extension Protocol v2 sidecars, plugin packages, marketplace | Out of D1's tier; full-trust sidecars are a large surface with a large security story. |
| Account/login, billing display currency, wallet balance, FX | Not a product goal for v1. |
| Telemetry upload and crash-report sending | We keep local diagnostics that help the user and drop the network paths (matching the "noticeable telemetry" debate honestly: fewer moving parts, no consent prompt needed). |
| `desktop/` (Studio), Wails-era theme packs | Out of scope entirely; the terminal needs a palette, not a pack format. |
| Browser/computer/LSP tool families | Large host-service dependencies (a Chromium, OS accessibility, spawning language servers) with low value for the first release. |
| Delegation, planner, goal mode | Out of D1's tier, and the reference's own measurements show delegation usually costs 2.4–4.5× tokens for the same outcome. |
| `use_capability`'s ledger/audit of MCP authorization | We keep a simplified proxy; the full audit ledger belongs with the extension runtime. |
