---
name: frontend-code-review
description: Review frontend code under `web/` or `packages/dify-ui/` when the user explicitly requests a review or audit. Supports PRs, branches, pending changes, files, and pasted diffs. Do not use for implementation-only requests, diagnosis without review intent, or backend-only code.
---

# Frontend Code Review

Review the requested scope for concrete defects and violations of explicit
project contracts.

## Evidence first

1. Resolve the requested target and scope. Include only the edits that belong
   to the request.
2. Read the changed code, its behavior owner and the applicable project rules.
   For a diff review, separate defects the change introduced from unrelated
   existing issues.
3. Trace affected consumers and contracts. When logic is removed or moved,
   identify the behavior it preserved and where that responsibility now lives.
   Check affected public types, examples and tests when they decide
   correctness.
4. Verify each candidate against actual defaults, guards, reachable states and
   documented exceptions. Report an observable failure, a security boundary
   violation or a concrete maintenance cost. Explicit team conventions are also
   contracts: establish their scope without inventing user impact.

## What to look at

- Semantics, naming, keyboard and focus, forms, pending states, responsive
  interaction.
- Component and module ownership, props, state, effects, navigation.
- Data access: queries, mutations, auth, URL state, persistence.
- Missing regression tests for a concrete defect.
- Bundle, waterfall, rendering or subscription cost supported by evidence.
- Type boundaries, styling composition, localized copy.

## Severity and output

- P0: critical impact needing immediate action (widespread unavailability,
  severe data loss, security or privacy breach).
- P1: major reachable failure of a primary workflow or public contract; blocks
  delivery.
- P2: narrower correctness, accessibility, performance or test defect; concrete
  maintenance cost; material violation of an explicit contract.
- P3: minor cleanup; omit unless a thorough audit was requested.

Choose severity by impact, reachability and recovery, not by topic;
accessibility failures are correctness defects. Lead with verified findings
ordered by severity, each with a tight file and line reference, the triggering
scenario or the rule, and a concrete fix direction. Deduplicate findings with
one cause. State material verification gaps separately; lack of verification
is not itself a defect. When nothing remains, say so briefly. No praise
sections and no speculative risks.
