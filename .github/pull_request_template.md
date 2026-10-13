<!--
One ticket per pull request. Each section below is a question a reviewer has to
answer anyway; writing the answer down is what makes the change reviewable a week
later, when nobody remembers why it looked like this. Comments can be deleted.

See docs/CONVENTIONS.md for how commits and pull requests work here.
-->

## Ticket

<!-- `closes #N` so that merging completes the ticket and the epic's sub-issue
     progress updates itself. -->

Epic **E?** · **E?-T? · <title>** — closes #N.

## What

<!-- What changed and why, in the reviewer's terms. The reasoning matters more
     than a restatement of the diff. -->

## Acceptance criteria

<!-- Copied verbatim from the ticket, so the reviewer can check the claim rather
     than take it. Tick what is met; where one cannot be met, say so here instead
     of ticking it. -->

- [ ] <criterion>

## Verification

<!-- The commands you actually ran, and what they said. A command you did not run
     does not belong here. If something could not be verified locally — a CI run,
     a hardware-specific path — say that here rather than leaving it implied. -->

```
```

## Practice audit (`GO-STANDARDS.md` §4)

<!-- Tier 1 (golangci-lint) result, the audit groups walked, and every finding as
     either fixed or accepted with a reason. "No findings" is a valid answer;
     silence is not. -->

## Dependencies

<!-- Required. Delete the line that does not apply. -->

- [ ] **No dependency was added, removed or changed.**
- [ ] A dependency was added, removed or changed, and `docs/DEPENDENCIES.md` carries the matching row in this PR.

## Scope notes

<!-- Optional, and often the most useful part: judgement calls worth a second
     pair of eyes, deviations from the ticket text, and anything deliberately
     left out. -->
