# pretty-reasonix

A clean-room, Charm-based terminal coding agent: it reads your project, edits
files, runs commands and tests, asks before dangerous things, and shows its work.

Inspired by the feature set of [Reasonix](https://github.com/esengine/DeepSeek-Reasonix),
**not derived from its code**.

## Design documents

| Document | What it is |
| --- | --- |
| [`docs/TECHNICAL_DESIGN.md`](docs/TECHNICAL_DESIGN.md) | The design of record: scope decisions, the reference analysis, the architecture, the engineering plan and the milestone/epic map. **Start here.** |
| [`docs/APPENDIX-A-REASONIX-REFERENCE.md`](docs/APPENDIX-A-REASONIX-REFERENCE.md) | The exhaustive inventory of the reference system — features, architecture, on-disk formats, CLI surface, tool list — annotated with which epic covers each, and an honest list of where its UX falls short. |
| [`docs/APPENDIX-B-BACKLOG.md`](docs/APPENDIX-B-BACKLOG.md) | The ticket backlog: 6 milestones, 22 epics, 187 v1 tickets with acceptance criteria, dependencies and sizes — plus a roadmap tier for everything deliberately deferred. |
| [`docs/GO-STANDARDS.md`](docs/GO-STANDARDS.md) | How the rules in [`go-best-practices.md`](go-best-practices.md) are enforced: what `golangci-lint` decides, what the post-implementation audit decides, and where we deliberately diverge. |
| [`docs/DEPENDENCIES.md`](docs/DEPENDENCIES.md) | The dependency policy: the rule for admitting a module, the approved set with one reason each, what is deliberately refused, and the review checklist. |
| [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) | How work reaches `master`: tickets, branches, commits, pull requests, and the optional local hooks. |

## Status

In progress. The scope is locked (TDD §3), and open work is tracked as issues on
the project board.

- **M0, the walking skeleton**, is mostly built: `prx run "task"` (or `prx -p`)
  answers through an OpenAI-compatible, Anthropic or Responses provider, using
  the read-only tools `read_file`, `ls`, `glob` and `grep`, and each session is
  written to disk as an event log.
- **M2's shell came before M1.** A bare `prx` opens the terminal UI, but the
  agent can only read: nothing yet writes a file or runs a command. Appendix B
  §B4.1 records the order from here, which puts the permission gate before any
  tool that writes.

## Shape

- **Language:** Go, single static binary, `CGO_ENABLED=0`.
- **TUI:** Bubble Tea v2 + Bubbles + Lip Gloss.
- **Surfaces in v1:** the interactive terminal UI and a headless `run`/`-p` over one controller.
- **Extensibility in v1:** MCP servers, skills, custom slash commands and hooks.

Command, flag, slash-command and configuration-key vocabulary mirrors Reasonix;
the code, the on-disk formats and the UX are our own.
