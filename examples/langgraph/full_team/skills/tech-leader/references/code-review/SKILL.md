---
name: code-review
description: "Review the changes since a fixed point (commit, branch, tag, or merge-base) along two axes: Standards (does the code follow this repo's documented coding standards?) and Spec (does the code match what the originating issue/spec asked for?). Runs both reviews in parallel sub-agents and reports them side by side. Use when the user wants to review a branch, a PR, work-in-progress changes, or asks to \"review since X\"."
---

Two-axis review of a change:

- **Standards**: does the code conform to the documented coding standards?
- **Spec**: does the code faithfully implement the originating spec?

Keep the axes separate. A change can pass one and fail the other: code that
follows every standard but implements the wrong thing is Standards pass, Spec
fail; code that does what was asked but breaks conventions is the reverse.
Reporting them separately stops one axis from masking the other.

## Spec axis

Report: (a) requirements the spec asked for that are missing or partial;
(b) behaviour that was not asked for (scope creep); (c) requirements that look
implemented but where the implementation looks wrong. Quote the spec line for
each finding. If no spec is available, say so.

## Standards axis

Report every place the change violates a documented standard, citing the rule,
plus any baseline smell below. Documented standards can be hard violations;
baseline smells are always judgement calls ("possible Feature Envy"), and a
documented repo standard overrides the baseline. Skip anything tooling already
enforces.

Smell baseline (what it is, then how to fix):

- **Mysterious Name**: a name that does not reveal what it does or holds. Rename.
- **Duplicated Code**: the same logic shape in more than one place. Extract it.
- **Feature Envy**: a method that uses another object's data more than its own. Move it.
- **Data Clumps**: the same few fields travel together. Bundle them into one type.
- **Primitive Obsession**: a primitive standing in for a domain concept. Give it a type.
- **Repeated Switches**: the same switch cascade recurs. Use polymorphism or one shared map.
- **Shotgun Surgery**: one logical change forces scattered edits. Gather what changes together.
- **Divergent Change**: one module edited for several unrelated reasons. Split it.
- **Speculative Generality**: abstraction added for needs the spec does not have. Delete it.
- **Message Chains**: long `a.b().c().d()` navigation. Hide the walk behind one method.
- **Middle Man**: a class or function that only delegates. Call the real target.
- **Refused Bequest**: a subclass that ignores most of what it inherits. Use composition.

## Aggregate

Present findings under `## Standards` and `## Spec`, without merging or
reranking. End with the total findings per axis and the worst issue within each
axis; do not pick one winner across axes.
