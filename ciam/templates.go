package ciam

// Templates are intentionally short: the value of these documents is the
// reasoning written into them, not the scaffolding.

const adrTemplate = `---
status: proposed
date: {{DATE}}
title: "{{TITLE}}"
---

# ADR-{{NUMBER}}: {{TITLE}}

## Context

<!-- What situation, problem or constraint forces a decision? -->

## Decision

<!-- What was decided, and why this option? Be direct. -->

## Consequences

### Positive

### Negative / trade-offs

## Alternatives considered

<!-- Other options and why they were not chosen. -->

## References
`

const prdTemplate = `---
status: draft
date: {{DATE}}
title: "{{TITLE}}"
---

# PRD-{{NUMBER}}: {{TITLE}}

## Problem

<!-- Which user or business problem are we solving? Why now? -->

## Goal

<!-- What must this achieve? How will success be measured? -->

## Scope

### In scope

### Out of scope

## Requirements

### Functional

### Non-functional

## Acceptance criteria

<!-- Verifiable: "given X, when Y, then Z". -->
- [ ] ...

## Dependencies

## References
`

const planTemplate = `---
status: draft
date: {{DATE}}
title: "{{TITLE}}"
prd: "PRD-000"
---

# Plan-{{NUMBER}}: {{TITLE}}

## Goal

<!-- Technical scope only; the "why" lives in the PRD. -->

## Phases

### Phase 1 - Foundation

**Automated success criteria:**
- [ ] ...

**Manual verification:**
- [ ] ...

_Pause here and verify before moving to Phase 2._

### Phase 2 - Validation

**Automated success criteria:**
- [ ] ...

**Manual verification:**
- [ ] ...

## Risks

## Rollback
`

const researchTemplate = `---
status: draft
date: {{DATE}}
title: "{{TITLE}}"
---

# RES-{{NUMBER}}: {{TITLE}}

## Question

## Findings

## Sources
`
