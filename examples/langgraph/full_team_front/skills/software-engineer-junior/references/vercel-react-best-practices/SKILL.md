---
name: vercel-react-best-practices
description: React and Next.js performance optimization guidelines from Vercel Engineering. This skill should be used when writing, reviewing, or refactoring React/Next.js code to ensure optimal performance patterns. Triggers on tasks involving React components, Next.js pages, data fetching, bundle optimization, or performance improvements.
license: MIT
metadata:
  author: vercel
  version: "1.0.0"
---

# Vercel React Best Practices

Performance guide for React and Next.js applications, maintained by Vercel,
prioritized by impact. Apply it when writing, reviewing or refactoring React
components, data fetching and bundles.

## Rule categories by priority

1. Eliminating waterfalls (CRITICAL)
2. Bundle size (CRITICAL)
3. Server-side performance (HIGH)
4. Client-side data fetching (MEDIUM-HIGH)
5. Re-render optimization (MEDIUM)
6. Rendering performance (MEDIUM)
7. JavaScript performance (LOW-MEDIUM)
8. Advanced patterns (LOW)

## 1. Eliminating waterfalls

- Check cheap synchronous conditions before awaiting flags or remote values.
- Move `await` into the branches where the value is used.
- Use `Promise.all()` for independent operations.
- Start promises early, await late in API routes.
- Use Suspense boundaries to stream content.

## 2. Bundle size

- Import directly, avoid barrel files.
- Prefer statically analyzable import paths.
- Use `next/dynamic` for heavy components.
- Load analytics and logging after hydration.
- Load a module only when its feature is activated.
- Preload on hover or focus for perceived speed.

## 3. Server-side performance

- Authenticate server actions like API routes.
- Use `React.cache()` for per-request deduplication, an LRU cache across requests.
- Minimize data passed to client components and avoid duplicate serialization.
- Avoid module-level mutable request state in RSC and SSR.
- Restructure components to parallelize fetches; use `after()` for non-blocking work.

## 4. Client-side data fetching

- Use SWR (or equivalent) for automatic request deduplication.
- Deduplicate global event listeners; use passive listeners for scroll.
- Version and minimize localStorage data.

## 5. Re-render optimization

- Do not subscribe to state that is only used in callbacks.
- Extract expensive work into memoized components; hoist default non-primitive props.
- Use primitive dependencies in effects.
- Derive state during render, not in effects; subscribe to derived booleans.
- Use functional `setState` for stable callbacks; lazy-initialize expensive state.
- Put interaction logic in event handlers, not effects.
- Use `startTransition` / `useDeferredValue` for non-urgent updates.
- Never define components inside components.

## 6. Rendering performance

- Use ternaries, not `&&`, for conditional rendering.
- Extract static JSX outside components.
- Use `content-visibility` for long lists.
- Prefer `useTransition` for loading state.
- Use `defer` or `async` on script tags.

## 7. JavaScript performance

- Build a `Map` or `Set` for repeated lookups.
- Combine multiple filter/map passes into one loop.
- Return early; hoist RegExp creation out of loops.
- Use `toSorted()` for immutability.
- Defer non-critical work to idle time.

## 8. Advanced patterns

- Store event handlers in refs; keep `useEffectEvent` results out of effect
  dependencies; initialize the app once per load.
