# Experiment: a local model as a subagent, answering through CodeGraph

**Date:** 2026-11-21 · **Harness:** Reasonix, this session · **Subject:** `task` subagent on a local provider

A single-question experiment with one purpose: **is delegating to the local model worth it, and is
`codegraph_explore` good enough to answer structural questions about this repository?**

The instruction was to delegate, not to analyse — and then to verify every claim myself rather than
accepting the subagent's answer. What follows is what it produced, unedited, and what checking it found.

## Setup

| | |
| --- | --- |
| Provider | `QwenSmallLocal` — an OpenAI-compatible endpoint on the LAN |
| Model | `Ternary-Bonsai-27B-PQ2_0.gguf`, one GGUF behind `llama-server` |
| Context | 16,384 (`-c`), after 8,192 proved too small to hold the harness prompt |
| MCP | CodeGraph, standard IO, indexed at `.codegraph/` (6.1 MB) |

The first attempt failed with `unknown model "local-qwncoder"` — the provider had been renamed since the
earlier experiment. Retried against `QwenSmallLocal`.

## Step 1 — the `READY` probe

**Instruction given:** `Reply with only the word READY.`

**Result:**

```
Subagent reference: sa_20261121_153717_000000000_d9b8e68fbccf
Final answer:
READY
```

**Not empty** — so thinking mode is off on the endpoint, and the answer is not stranded in
`reasoning_content`. This was the failure the probe existed to catch, and it did not occur.

## Step 2 — the delegated question

**Instruction given, verbatim:**

> Use codegraph_explore only. Explain how internal/architecture/arch_test.go enforces layering. Answer
> these four things: (a) the layers in order, (b) what isolatedLayers contains and what the rule means for
> imports, (c) what the exclusive groups are and what they forbid, (d) what namedEdges is for. One level of
> detail. Do not read source files directly, do not trace into other packages. Cite file:line for each claim.

**What came back, unedited:**

> ### (a) The layers, in order
>
> Six ranks from rank 0 to 6 ([L30–L52]):
>
> | Rank | Name / Role | Packages |
> |------|-------------|----------|
> | **0** | The contract — interfaces and wire types (and the test that guards them) | `internal/contract`, `internal/architecture` |
> | **1** | Foundations — config, storage, safety, helpers | `internal/config`, `internal/session`, `internal/perm`, `internal/sandbox`, `internal/model`, `internal/httpclient`, `internal/i18n`, `internal/version` |
> | **2** | Capabilities — implement the contract, know nothing of the loop | `internal/tool`, `internal/provider`, `internal/ext` |
> | **3** | The turn loop and its context maintenance | `internal/agent`, `internal/compaction` |
> | **4** | Wiring — the one place that names a concrete implementation | `internal/assembly` |
> | **5** | Frontends — drive the controller, nothing drives them | `internal/frontend` |
> | **6** | Process entry point (calls only the CLI layer) | `cmd` |
>
> ### (b) `isolatedLayers`
>
> It contains a single entry: `["internal/contract"]` ([L59]).
>
> **Rule:** A package inside `internal/contract` must import **nothing** from outside itself. The boundary is
> the layer (the whole subpackage), not any individual subpackage. The test checks this in `breach()` by
> scanning whether any package under an isolated layer imports a package outside it, returning the failure
> message at [L118–L121].
>
> ### (c) Exclusive groups
>
> One exclusive group: `{"internal/tool", "internal/provider", "internal/ext"}` ([L64]).
>
> **What it forbids:** No capability package may import any sibling capability package in either direction.
> They share the contract (rank 0) as their vocabulary, not each other's internals. The test checks this at
> [L124–L134], scanning every edge whose `from` belongs to the group and flags any whose `to` also belongs to
> a different member.
>
> ### (d) `namedEdges`
>
> It lists four explicitly-allowed edges that would otherwise violate the layering rule — [L68–L82]:
>
> | From | To | Why allowed |
> |------|-----|-------------|
> | `internal/frontend/termrender` | `internal/agent` | "the renderer draws values; it must not know the loop exists" |
> | `internal/frontend/termrender` | `internal/session` | "the renderer holds no session state" |
> | `internal/perm` | `internal/agent` | "the agent consults a Gate; policy never reaches up into the loop" |
> | `internal/sandbox` | `internal/agent` | "the sandbox is data the agent supplies, not a caller of it" |
>
> They are checked as explicit exceptions — if an edge matches one of these `from`→`to` pairs, `breach()`
> returns the documented reason rather than a violation — [L136–L140]. The test's
> `TestBreachRecognisesTheDocumentedViolations` ([L407–L444]) uses these as positive test cases.

## Step 3 — verification

Every claim checked against the file, plus:

```
grep -n "isolatedLayers\|exclusive = \|namedEdges" internal/architecture/arch_test.go
54:// isolatedLayers import nothing from outside themselves. …
59:var isolatedLayers = []string{"internal/contract"}
63:var exclusive = [][]string{
67:// namedEdges are the absolutes §6.2 states directly, which direction alone would
69:var namedEdges = []struct {
118:	for _, layer := range isolatedLayers {
136:	for _, rule := range namedEdges {
```

### The verdict on each part

| | Claim | Verdict |
| --- | --- | --- |
| **(a)** | Six ranks 0–5, contract / foundations / capabilities / loop / wiring / frontends | **CORRECT**, and better than asked — it found **rank 6 = `cmd`**, `internal/architecture` at rank 0, and all eight rank-1 packages |
| **(b)** | `isolatedLayers = ["internal/contract"]`; the boundary is the layer; members import nothing from outside it | **CORRECT** — line 59, and the rule described is the one the code applies |
| **(c)** | One exclusive group `{tool, provider, ext}`; siblings may not import each other in either direction | **CORRECT** — line 63; `breach` iterates exactly that |
| **(d)** | `namedEdges` lists four **explicitly-allowed** edges | **WRONG — inverted** |

### The one error, precisely

It said the four edges are "checked as explicit exceptions — if an edge matches one of these `from`→`to`
pairs, `breach()` returns the documented reason **rather than a violation**."

`breach()` is the function that decides *whether something is a violation*. Line 136 sits inside it:

```go
for _, rule := range namedEdges {
    if under(from, rule.from) && under(to, rule.to) {
        return rule.why        // a breach, with the reason attached
    }
}
```

And the declaration says so outright (L67–68): *"the absolutes §6.2 states directly, which direction alone
would **allow** because the target sits lower in the graph."* The target sits lower — ranking would permit
the edge — and the rule exists to **forbid it anyway**.

**It quoted the data perfectly and read the direction of the mechanism backwards.** The pair list, all four
`why` strings and the line number are right; the word "allowed" is exactly wrong.

### Citation audit

| Citation | Claimed | Actual | |
| --- | --- | --- | --- |
| layer table | `L30–L52` | 28–52 | ✓ |
| `isolatedLayers` | `L59` | 59 | ✓ |
| `exclusive` | `L64` | **63** | one off |
| `namedEdges` | `L68–L82` | 67–82 | close |
| isolated check | `L118–L121` | 118+ | ✓ |
| namedEdges check | `L136–L140` | 136+ | ✓ |
| the test | `L407–L444` | **405** (file is 445) | one off |

**Zero invented citations.** Nothing pointed at a line that does not exist or at a symbol that is not there.
Two were off by one, and one range started a line early.

**Format deviation:** the brief asked for `file:line`; it cited line ranges in brackets (`[L59]`), with no
filename in most cells.

## Failure mode

**Stopped early — in one specific way.** It answered all four parts in a single pass with no follow-up tool
calls. For (d) that was fatal: one more read of the *enclosing function* would have shown `breach` returning
a violation. It read the variable declaration, and inferred semantics from the field name `why` plus the
imperative phrasing of the reasons — a plausible inference, and the wrong one.

**Looped: no.** One round, no repetition, no restating.

**Read files directly: not observable.** But the `[L…]` citation style and the "one level of detail"
compliance suggest it took a single `codegraph_explore` result and reasoned over it, rather than walking
symbols.

**Unprompted extra:** it volunteered that the file is 445 lines and that the test at `L407–L444` "uses these
as positive test cases" — a claim about a test it never read.

## Short verdict

**Worth it for the facts, not for the verdict.**

Eighty percent of the work — the layer table, the isolated rule, the exclusive group, the exact reason strings
— arrived in one round, correct, in a shape that took two reads to verify instead of eight. That is a real
saving.

**But the one thing it got wrong is the one thing worth delegating.** "Is `namedEdges` a whitelist or a
blacklist?" is the *interpretive* question, and interpretation is where a summariser fails: it had every token
needed to be right, and inverted the semantics because the field name and the grammar of the comment read like
permission.

**And the deeper lesson matches the four seams found in this project.** The model produced a coherent,
well-formatted, correctly-cited answer — and the sentence that mattered was wrong. **Nothing about the
presentation distinguished the correct parts from the inverted one.** That is the same shape as a green test
suite hiding an unwelded seam: the confidence of the output carries no information about its truth.

Would I have been faster myself? For (a)–(c), yes — three reads saved. For (d), no: I would have read `breach`
directly and been right the first time. **Net: a wash on time, a gain on structure, and a reminder that
"verify what comes back" is not a formality — it is the whole method.**
