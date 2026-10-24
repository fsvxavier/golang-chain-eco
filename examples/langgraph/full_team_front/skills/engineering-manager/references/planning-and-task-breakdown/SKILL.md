---
name: planning-and-task-breakdown
description: Breaks work into ordered tasks. Use when you have a spec or clear requirements and need to break work into implementable tasks. Use when a task feels too large to start, when you need to estimate scope, or when parallel work is possible.
---

# Planning and Task Breakdown

Decompose work into small, verifiable tasks with explicit acceptance criteria.
Every task should be small enough to implement, test, and verify in a single
focused session.

## When to Use

- You have a spec and need to break it into implementable units.
- A task feels too large or vague to start.
- Work needs to be parallelized across several workers.
- The implementation order is not obvious.

Not for single-file changes with obvious scope, or a spec that already holds
well-defined tasks.

## The Planning Process

1. Plan read-only: read the spec and relevant code, identify existing patterns,
   map dependencies between components, note risks and unknowns. Do not write
   code while planning.
2. Identify the dependency graph (schema, types, endpoints, API client, UI
   components, validation). Order work bottom-up: foundations first.
3. Slice vertically. Instead of all schema, then all API, then all UI, build
   one complete feature path at a time ("user can log in": schema + API + UI).
   Each vertical slice delivers working, testable functionality.
4. Write each task with: a short title, a one-paragraph description,
   acceptance criteria (specific, testable), verification (tests, build,
   manual check), dependencies, files likely touched, estimated scope.
5. Order and checkpoint: dependencies satisfied first; each task leaves the
   system working; a verification checkpoint after every 2-3 tasks; high-risk
   tasks early (fail fast).

## Task Sizing

| Size | Files | Scope |
|------|-------|-------|
| XS | 1 | single function or config change |
| S | 1-2 | one component or endpoint |
| M | 3-5 | one feature slice |
| L | 5-8 | multi-component feature |
| XL | 8+ | too large, break it down further |

Break a task down further when it would take more than one focused session,
when its acceptance criteria need more than three bullets, when it touches two
independent subsystems, or when its title contains "and". Workers do best on S
and M tasks.

## Parallelization

- Safe to parallelize: independent feature slices, tests for finished
  features, documentation.
- Must be sequential: migrations, shared state changes, dependency chains.
- Needs coordination: features that share an API contract (define the contract
  first, then parallelize).

## Red Flags

- Tasks that say "implement the feature" without acceptance criteria.
- No verification step in a task.
- All tasks XL-sized.
- No checkpoints between tasks.
- Dependency order not considered.

## Verification

- [ ] Every task has acceptance criteria and a verification step.
- [ ] Dependencies are identified and ordered correctly.
- [ ] No task touches more than about 5 files.
- [ ] Checkpoints exist between major phases.
