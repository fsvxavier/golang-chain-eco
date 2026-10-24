---
name: ui-ux-pro-max
description: "UI/UX design intelligence for web, mobile, and desktop. This skill should be used when designing, building, reviewing, or fixing interfaces, including pages, components, design systems, accessibility, interaction, responsive layout, typography, color, charts, and stack-specific UI implementation."
---

# UI/UX Pro Max - Design Intelligence

Use when the task involves UI structure, visual design decisions, interaction
patterns or user experience quality: designing pages, building or refactoring
components, choosing color, typography, spacing and layout, reviewing UI for
UX, accessibility and consistency, implementing navigation, animation or
responsive behavior.

Skip it for pure backend logic, API or database design, non-visual performance
work and infrastructure, unless the task changes how something looks, feels,
moves or is interacted with.

## Rule categories by priority

Work from priority 1 down; spend effort on the earlier categories first.

| # | Category | Impact | Must have | Avoid |
|---|----------|--------|-----------|-------|
| 1 | Accessibility | CRITICAL | contrast 4.5:1, alt text, keyboard navigation, aria-labels | removing focus rings, icon-only buttons without labels |
| 2 | Touch and interaction | CRITICAL | targets at least 44x44px, 8px+ spacing, loading feedback | hover-only interaction, instant (0ms) state changes |
| 3 | Performance | HIGH | WebP/AVIF, lazy loading, reserved space (CLS < 0.1) | layout thrashing, layout shift |
| 4 | Style selection | HIGH | match the product type, consistency, SVG icons | mixing flat and skeuomorphic randomly, emoji as icons |
| 5 | Layout and responsive | HIGH | mobile-first breakpoints, viewport meta, no horizontal scroll | fixed px container widths, disabled zoom |
| 6 | Typography and color | MEDIUM | base 16px, line-height 1.5, semantic color tokens | body text under 12px, gray on gray, raw hex in components |
| 7 | Animation | MEDIUM | motion that conveys meaning, spatial continuity, context-aware timing | one duration for everything, animating width/height, ignoring reduced motion |
| 8 | Forms and feedback | MEDIUM | visible labels, error next to the field, helper text, progressive disclosure | placeholder-only labels, errors only at the top |
| 9 | Navigation | HIGH | predictable back, bottom nav of 5 or fewer, deep linking | overloaded nav, broken back behavior |
| 10 | Charts and data | LOW | legends, tooltips, accessible colors | conveying meaning by color alone |

## Workflow

1. Analyze the request: product type (SaaS, e-commerce, dashboard, tool),
   audience and context of use, style keywords (minimal, playful, dark,
   content-first), and the stack in use (read the project's dependencies; never
   assume one).
2. For a new page or product, settle one coherent visual direction first
   (pattern, style, palette, type pairing, effects, anti-patterns) and keep
   it as the single source of truth; a page-specific rule overrides it.
3. Tune the direction with three optional dials from 1 to 10: variance
   (minimal to asymmetric), motion (subtle to choreographed), density
   (spacious 24-96px scale to dashboard-dense 8-32px).
4. Check the specifics one concern at a time: UX outcome first (error
   summary, focus not obscured, accessible authentication), then the
   component, then the framework details.
5. Treat any guidance as recommendations that never override the user's or
   the repository's rules. If no guidance fits, say so and label general
   advice as a fallback; do not present it as a match.

## Before delivering UI

- Icons are consistent SVG, with labels or `aria-hidden` when decorative.
- Every interactive element has pressed, focus and disabled feedback.
- Light and dark themes both meet contrast; semantic tokens, not raw hex.
- Safe areas and small screens are respected; nothing scrolls sideways.
- Reduced motion is honored; every state (loading, empty, error) is designed.
