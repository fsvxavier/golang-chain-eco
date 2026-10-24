---
name: playwright-best-practices
description: Use when writing Playwright tests, fixing flaky tests, debugging failures, implementing Page Object Model, mocking APIs, testing accessibility (axe-core), forms and validation, error and loading states, and organizing a test suite. Covers E2E, component, API and accessibility testing.
license: MIT
metadata:
  author: currents.dev
  version: "1.2"
---

# Playwright Best Practices

Guidance for writing and maintaining Playwright tests. This is a trimmed
subset of the upstream skill, curated for writing tests from acceptance
criteria: locators, assertions and waiting, test structure, page objects,
flakiness, accessibility, and error and loading states.

## Locators: priority order

1. Role: `getByRole("button", { name: /submit/i })` (most resilient; matches
   how users and assistive technology see the page)
2. Label and placeholder: `getByLabel`, `getByPlaceholder`
3. Text and title: `getByText`, `getByTitle`
4. Test id: `getByTestId` when no semantic locator works
5. CSS or XPath: last resort

Useful roles: button, link, textbox, checkbox, radio, combobox, heading (with
`level`), list and listitem, navigation, main, dialog, alert. Narrow with
`filter({ hasText })`, `filter({ hasNotText })`, `filter({ has: locator })`,
chain `getByRole("article").getByRole("heading")`, and use
`first()`, `last()`, `nth(i)` only when the position matters.

## Assertions and waiting

- Use web-first assertions, which retry until they pass or time out:
  `await expect(locator).toBeVisible()`, `toHaveText`, `toHaveValue`,
  `toBeEnabled`, `toHaveCount`, `expect(page).toHaveURL(...)`.
- Do not assert on a value read once (`expect(await locator.textContent())`);
  it has no retry.
- Let auto-waiting work. Wait for a specific condition when you must:
  `waitForResponse`, `waitForURL`, a locator state.
- Poll with `expect(async () => ...).toPass()` or `expect.poll(...)`, not
  hand-written retry loops.
- Soft assertions (`expect.soft`) report several failures in one run.

| Do | Don't |
|----|-------|
| web-first assertions | generic assertions on the DOM |
| auto-waiting | `waitForTimeout()` or `setTimeout` sleeps |
| specific conditions | waiting for arbitrary time |
| fix the timing issue | raise the global timeout to hide a flake |

## Test structure

- One behavior per test, independent of execution order; no shared mutable
  state between tests.
- Group by feature with `test.describe`; keep files short and split by feature.
- Use `beforeEach` and fixtures for setup; give each test its own data.
- Name a test after the requirement it covers (for example `AC2: expired coupon
  is rejected`), so a failure maps to the specification.
- Use tags (`@smoke`, `@critical`) for filtered runs and `test.fixme` or
  `test.skip` with a reason for known gaps; never delete a failing test to go
  green.

## Page objects

Encapsulate page structure and interactions in a class: locators defined in
the constructor, one method per user intent, assertions left in the tests.
Selectors then change in one place, tests read as intent, and interactions are
reused. Compose pages from smaller component objects (header, dialog, table)
instead of one huge class. Prefer fixtures when the object only needs setup.

## Flaky tests

| Category | Symptom | Usual cause |
|----------|---------|-------------|
| UI-driven | element not found, missed click | missing waits, animations, dynamic rendering |
| Environment | fails only in CI | slower CPU, cold browser |
| Data or parallelism | fails with several workers | shared backend data, reused accounts |
| Suite | fails only with other tests | leaked state, order dependence |

Reproduce locally (repeat the test), read the trace, then fix the cause. Mock
external services; isolate data per worker; retries are for diagnosis, not a
cure. Quarantine and track a flaky test instead of ignoring it.

## Accessibility checks

With `@axe-core/playwright`, assert that
`new AxeBuilder({ page }).analyze()` has `violations` equal to `[]`; scope it
with `.include(selector)` and exclude only known, documented issues. Test the
real flows, not only the home page. Also test by hand-written steps: Tab order
reaches every control, Enter and Space activate, Escape closes dialogs, focus
moves into a dialog and returns to its trigger, errors are exposed through
`role="alert"` or `aria-live`, `prefers-reduced-motion` is honored
(`page.emulateMedia({ reducedMotion: "reduce" })`). Automated checks catch
only a subset; say so.

## Error, loading and empty states

Cover more than the happy path:

- API errors: `page.route("**/api/**", r => r.fulfill({ status: 500 }))`, then
  assert the visible error message and the recovery action.
- Slow responses: delay the route and assert the loading indicator or
  skeleton appears and then disappears.
- Empty results: fulfill with an empty list and assert the empty state.
- Failed or aborted requests (`route.abort()`) and offline mode
  (`context.setOffline(true)`) and recovery.
- Forms: client-side validation (required, format), server-side validation
  errors shown next to the field, focus on the first error, no double submit.

## Anti-patterns

| Anti-pattern | Fix |
|--------------|-----|
| Only the happy path | test every error scenario in the spec |
| Skipping loading states | assert the loading UI appears |
| Long test files, many features per test | split by feature, one behavior per test |
| Tests depending on order | keep them independent |
| Accessibility tested only on one page | test all critical flows |
| Real external APIs | mock them |
