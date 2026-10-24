---
name: react-testing
description: React component testing with React Testing Library, Vitest/Jest, MSW for network mocking, accessibility assertions with axe, and the decision boundary between component tests and Playwright/Cypress end-to-end runs. Use when writing or fixing tests for React components, hooks, or pages.
metadata:
  origin: ECC
---

# React Testing

Behavior-focused component tests, custom hook tests, accessibility assertions
and network-level mocking.

## Core principle

Test what the user sees and does, not implementation details.

A test renders the component with the providers it has in production,
interacts through accessible queries (role, label) and `userEvent`, and asserts
visible output and observable side effects (callback fired, request sent).

A test does not inspect component state, props passed to children or which
hooks ran; does not mock React; does not count renders.

## Query priority (top-down)

1. `getByRole`, `getByLabelText`, `getByPlaceholderText`, `getByText`,
   `getByDisplayValue`
2. `getByAltText`, `getByTitle`
3. `getByTestId` as the last resort

Variants: `getBy*` throws if absent; `queryBy*` returns `null` (assert
absence); `findBy*` is async (elements that appear after async work).

```tsx
screen.getByRole("button", { name: /save/i });
```

## User interaction

```tsx
test("submits the form", async () => {
  const user = userEvent.setup();
  const onSubmit = vi.fn();
  render(<UserForm onSubmit={onSubmit} />);
  await user.type(screen.getByLabelText("Email"), "user@example.com");
  await user.click(screen.getByRole("button", { name: /save/i }));
  expect(onSubmit).toHaveBeenCalledWith({ email: "user@example.com" });
});
```

Always `await` userEvent calls; call `userEvent.setup()` once per test; prefer
`userEvent` over `fireEvent`.

## Async

Use `await screen.findByText(...)`, `await waitFor(() => expect(...))` and
`waitForElementToBeRemoved(...)`. Never `setTimeout` plus an assertion.

## Network mocking with MSW

Mock at the network layer so the component, hooks and fetch library behave as
in production. Start the server with `onUnhandledRequest: "error"` so an
unmocked request fails loudly, `resetHandlers` after each test, `close` after
all. Override per test to cover the failure states:

```tsx
server.use(http.get("/api/users/:id", () => new HttpResponse(null, { status: 500 })));
render(<UserPage id="1" />);
expect(await screen.findByText(/something went wrong/i)).toBeInTheDocument();
```

Cover every UI state: loading (`getByText("Loading...")` then content), empty,
error.

## Providers and hooks

Wrap providers once in a `renderWithProviders` helper (query client with
`retry: false`, theme, router). For hooks use `renderHook` with a `wrapper`,
wrap state changes in `act`, and create the `QueryClient` once per test outside
the wrapper so the cache survives re-renders.

## Accessibility assertions

Run `axe` (`jest-axe` or `vitest-axe`) on every interactive component:
`expect(await axe(container)).toHaveNoViolations()`. It catches missing labels,
invalid ARIA, missing alt text and heading order. JSDOM has no real CSS engine,
so visual contrast belongs in a real browser.

## Snapshots

Avoid DOM snapshots: they break on every styling change and get rubber-stamped.
Acceptable only for pure serialization functions.

## Component test or browser test

JSDOM cannot render real layout, animations, scrolling, drag-and-drop, iframes
or downloads. Hook, presentational component or form with logic: RTL.
Layout-dependent component: Playwright component testing. Full flow across
pages: end-to-end.

## Anti-patterns

- `container.querySelector(...)`: bypasses accessible queries.
- Asserting on the number of renders.
- `jest.mock("react")`: refactor the component instead.
- Mocking child components by default.
- Ignoring `act()` warnings: they signal real bugs.
- Sharing mutable state across tests.

## TDD

Red (failing test for the next requirement), green (minimal code), refactor,
repeat. Verify a new test fails for the right reason before implementing.
