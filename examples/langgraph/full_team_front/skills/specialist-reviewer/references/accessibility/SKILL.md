---
name: accessibility
description: Audit and improve web accessibility following WCAG 2.2 guidelines. Use when asked to "improve accessibility", "a11y audit", "WCAG compliance", "screen reader support", "keyboard navigation", or "make accessible".
license: MIT
metadata:
  author: web-quality-skills
  version: "2.0"
---

# Accessibility (a11y)

Guidelines based on WCAG 2.2: make content usable by everyone, including people
with disabilities. Target level AA (A must pass).

## Evidence-led audit

1. Localize the problem to a component from failed audit nodes, not by
   searching for generic patterns.
2. Inspect names, roles, states, landmarks and heading structure, and exercise
   the affected flow with the keyboard.
3. Fix the source, then repeat the same check. Automated tools find only a
   subset of barriers; a perfect score is not WCAG conformance.

## Principles (POUR)

Perceivable, Operable, Understandable, Robust.

## Perceivable

- Images need `alt`; decorative images use `alt=""`. Icon buttons need an
  accessible name (`aria-label`, or visually hidden text) and the icon gets
  `aria-hidden="true"`.
- Contrast (1.4.3): normal text 4.5:1, large text 3:1, UI components and
  graphics 3:1. Focus indicators also need 3:1 (1.4.11).
- Do not rely on color alone: an error is color plus icon plus text, with
  `aria-invalid="true"` and `aria-describedby` pointing at the message.
- Video needs captions; audio needs a transcript.

## Operable

- Everything works with the keyboard. Prefer native `<button>`, `<a href>` and
  form controls. A non-native widget needs `role`, `tabindex="0"` and
  Enter/Space handling (never add that to a native button: it double-fires).
- No keyboard traps; dialogs trap focus only while open (native `<dialog>`
  does it) and restore focus on close.
- Focus must be visible (2.4.7): never `outline: none` without a replacement;
  use `:focus-visible`.
- Focus not obscured (2.4.11): sticky headers must not hide the focused
  element (`scroll-margin-top`).
- Provide a skip link past repeated navigation (2.4.1).
- Target size at least 24x24 CSS px (2.5.8), 44x44 recommended.
- Any drag action has a single-pointer alternative (2.5.7).
- Let users extend time limits; honor `prefers-reduced-motion: reduce`.

## Understandable

- `lang` on `<html>`, and on changes of language.
- Navigation and help placement consistent across pages (3.2.3, 3.2.6).
- Every input has a programmatically associated label (3.3.2).
- Errors: announce with `role="alert"` or `aria-live`, set `aria-invalid`,
  focus the first error on submit (3.3.1, 3.3.3).
- Do not make users re-enter data they already gave (3.3.7).
- Authentication must not depend on a cognitive test: allow paste and
  autofill, offer passkey or email link (3.3.8).

## Robust

- Prefer native elements over ARIA roles on `div`s.
- When ARIA is needed, use correct roles and states.
- Announce dynamic changes with `aria-live` regions without moving focus.

## Manual checklist

- [ ] Tab through the whole page; Enter and Space activate controls.
- [ ] Screen reader pass (VoiceOver, NVDA or TalkBack).
- [ ] Usable at 200% zoom and with high contrast.
- [ ] Reduced motion respected.
- [ ] Focus order is logical and follows the visual order.

## Issues by impact

- Critical: missing form labels, missing alt text, insufficient contrast,
  keyboard traps, no focus indicators.
- Serious: missing page language, heading structure, non-descriptive link
  text, auto-playing media, no skip link.
- Moderate: missing ARIA labels on icons, inconsistent navigation, missing
  error identification, timing without controls, missing landmarks.
