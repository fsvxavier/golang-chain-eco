---
name: frontend-design
description: Guidance for distinctive, intentional visual design when building new UI or reshaping an existing one. Helps with aesthetic direction, typography, and making choices that don't read as templated defaults.
license: Complete terms in LICENSE.txt
---

# Frontend Design

Approach the work as the design lead at a studio known for giving every client
a distinct visual identity. Make deliberate, opinionated choices about palette,
typography and layout that are specific to this brief.

## Ground the design in the subject

Identify the product, its audience and the screen's primary job before
designing. Industry, materials and vernacular are where distinctive choices
come from: a toy for children and a dashboard for financial analysts look very
different. Use the brief's real content throughout.

## Principles

- Typography carries personality. Use one family or two, clearly distinct, and
  a clear type scale with intentional weights and spacing. Keep line lengths
  under 80 characters.
- Avoid the commonest generated-page tells: accenting a single word in a
  headline, all-caps labels, unnecessary labels above content.
- Visual structure is information. Borders, numbering and dividers must encode
  something. Number markers only when the content is a sequence.
- Use non-user-triggered motion sparingly: one orchestrated moment beats
  scattered effects. Motion that answers a user action is welcome.
- Spend boldness in one place; keep everything around it quiet. Cut any
  decoration that does not serve the brief.

## Process

1. Plan first: a compact token system with 4-6 named hex colors, type roles,
   a layout concept (alignment included) and the principles that make the page
   unique.
2. Review the plan against the brief. If a part reads like the default you
   would produce for any similar page (cream background with serif display;
   near-black with one acid accent; identical rounded cards with the same soft
   shadow; tracked all-caps eyebrow above every heading), revise it.
3. Build. Watch CSS selector specificity so classes do not cancel each other.
4. Quality floor: responsive down to mobile, visible keyboard focus, reduced
   motion respected, accessible color contrast.

## Writing in design

Words exist to make the interface easier to use.

- Write from the user's perspective: they manage notifications, not webhook
  config. Be specific and plain, not clever.
- Active voice. A button says what happens ("Save changes", not "Submit"),
  and an action keeps its name through the whole flow ("Publish" then
  "Published").
- Failure and emptiness give direction, not mood: say what went wrong and how
  to fix it; an empty screen invites an action. Errors do not apologize.
- Sentence case, plain verbs, no filler.
