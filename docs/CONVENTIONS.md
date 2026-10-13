# Conventions

How work reaches `master`. Companion to [`GO-STANDARDS.md`](./GO-STANDARDS.md)
(how Go code is written), [`DEPENDENCIES.md`](./DEPENDENCIES.md) (what may be
depended on) and [`APPENDIX-B-BACKLOG.md`](./APPENDIX-B-BACKLOG.md) (what the
work is).

## Tickets, branches and commits

The backlog document is the source of truth; the GitHub issues are its
projection. A change that alters what a ticket means changes the document first,
then the issue.

- **One ticket per branch.** `e<epic>-t<n>/<slug>`, branched from `master`, for
  example `e1-t7/pr-template`.
- **One reviewable change per commit.** If a commit cannot be reviewed in one
  sitting, it is two commits.
- **The subject names the ticket:** `E1-T7: add the PR template and the commit
  hook`. The ID is what makes a commit findable from its ticket and back.
- **The body carries the why, not the what.** The diff already says what. What
  belongs in the body is the reasoning, the alternatives rejected, and the
  decisions a reviewer should question.
- **The body ends with a `Verified:` line** naming the commands that were run.
  It is the commit-sized version of the pull request's verification section.
- **Fix up within the branch rather than piling on.** A review round that ends in
  `fix review comments` commits should be squashed before merge, so `master`
  reads as deliberate.

## Pull requests

The template is [`.github/pull_request_template.md`](../.github/pull_request_template.md),
and its fields are the four questions a reviewer asks: what is this, does it meet
its criteria, was it checked, and what did it cost.

- **`closes #N`** in the body, so merging completes the ticket and the epic's
  sub-issue progress updates without anyone remembering to do it.
- **The acceptance criteria are copied verbatim**, so the reviewer checks a claim
  instead of trusting a summary.
- **The verification section lists commands that were actually run.** "It builds"
  is not verification; a command and its output is.
- **The practice audit gets an answer for every finding** — fixed, or accepted
  with a stated reason. `GO-STANDARDS.md` §7 is where a deliberate divergence
  from a baseline rule is recorded, and a divergence with no entry there is the
  one outcome that is never acceptable.
- **The dependency question is answered either way.** Most changes answer "no";
  the point of asking every time is that the answer is a claim someone can check
  against `go.mod`.

## Hooks

[`.githooks/pre-commit`](../.githooks/pre-commit) checks formatting on the staged
Go files. It is opt-in, because `core.hooksPath` is a per-clone setting and this
repository does not write to your git configuration:

```sh
git config core.hooksPath .githooks
```

It is deliberately **only** formatting. CI is the gate — `make lint`,
`go test -race ./...` on two platforms, the cross-compile matrix — and a hook
that duplicated all of it would slow every commit for feedback you get a minute
later anyway. Unformatted code is the one failure that is cheap to catch here and
merely irritating to learn about from a red build.

Nothing in `.githooks/` runs unless you enable it. Disable it with
`git config --unset core.hooksPath`.
