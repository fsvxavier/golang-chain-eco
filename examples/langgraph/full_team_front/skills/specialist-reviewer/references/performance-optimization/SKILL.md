---
name: performance-optimization
description: Optimizes application performance across frontend, backend, queries, and databases. Use when performance requirements exist, when you suspect performance regressions, when Core Web Vitals or load times need improvement, when N+1 query patterns need fixing, or when profiling reveals bottlenecks.
---

# Performance Optimization

Measure before optimizing. Performance work without measurement is guessing.
Profile first, identify the actual bottleneck, fix it, measure again. Optimize
only what measurements prove matters. In a review, flag optimization offered
without evidence as readily as an obvious regression.

## Core Web Vitals targets

| Metric | Good | Needs improvement | Poor |
|--------|------|-------------------|------|
| LCP (Largest Contentful Paint) | 2.5s or less | up to 4.0s | over 4.0s |
| INP (Interaction to Next Paint) | 200ms or less | up to 500ms | over 500ms |
| CLS (Cumulative Layout Shift) | 0.1 or less | up to 0.25 | over 0.25 |

## Workflow

1. MEASURE: baseline with real data (synthetic for reproducibility, field data
   to confirm a fix helped users).
2. IDENTIFY: the actual bottleneck, not an assumed one.
3. FIX: that one thing.
4. VERIFY: measure again the same way; keep or revert.
5. GUARD: a budget or monitor so it cannot regress silently.

Where to start by symptom: first load (bundle size, server response, render
blocking resources); sluggish interaction (long main-thread tasks over 50ms,
re-renders, layout thrashing); slow after navigation (API waterfalls, N+1
fetches).

## Frontend bottlenecks

| Symptom | Likely cause |
|---------|--------------|
| Slow LCP | large images, render-blocking resources, slow server |
| High CLS | images without dimensions, late content, font shifts |
| Poor INP | heavy JavaScript on the main thread, large DOM updates |
| Slow initial load | large bundle, many requests |

Fixes:

- Every image declares `width` and `height`; the LCP image gets
  `fetchpriority="high"` and modern formats; below-the-fold images get
  `loading="lazy"` and `decoding="async"`; use `srcset`/`sizes`.
- An object or function created in render is a new reference every time. Hoist
  constants; use `React.memo` and `useMemo` only for work the profile shows is
  expensive.
- Route-level code splitting and lazy loading of heavy, rarely used features
  behind `Suspense` beat changing import styles.

## Verify: keep or revert

- Re-measure under the same conditions as the baseline (a cold-cache baseline
  against a warm-cache result measures the cache).
- Change one thing at a time.
- Beat the noise, not just the mean: a 3% gain inside 5% variance is no gain.

| Result vs baseline | Action |
|---|---|
| Past the threshold, tests green | keep, with before/after numbers |
| Within noise | revert |
| Worse | revert |
| Improved but a test went red | revert |

Neutral is a revert: code you keep, you maintain. Correctness gates the metric;
a win that drops needed work (a validation, a load-bearing `await`) is a
regression. Log every attempt, reverted ones included, so a dead idea is not
re-tried.

## Guard

Guard the metric users feel (LCP, INP, p95 latency) with a synthetic budget in
CI and p75 field monitoring. Example budgets: initial JavaScript under 200KB
gzipped, CSS under 50KB, above-the-fold images under 200KB, fonts under 100KB,
API p95 under 200ms.

## Red flags

- Optimization without profiling data.
- Images without dimensions, lazy loading or responsive sizes.
- Bundle size growing without review.
- `React.memo` and `useMemo` everywhere.
- Several optimizations bundled into one measurement.
- A "win" that needed a test changed, skipped or deleted.

## Verification

- [ ] Before and after numbers exist, measured the same way.
- [ ] The improvement exceeds run-to-run variance.
- [ ] Neutral changes were reverted.
- [ ] Core Web Vitals are within the Good thresholds; bundle size has not
      grown significantly.
- [ ] Existing tests still pass.
