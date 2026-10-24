# Coding standards

## Components
Build small components with one job. Use semantic elements (button, label,
input, nav, form) before reaching for a div with a role. Keep state as close to
where it is used as possible, and derive values (a total, a discount) from the
data instead of copying them into state.

## UI states
Every screen that loads data renders four states: loading, empty, error and
success. The error state shows a message the user can act on and a way to retry.
A control that starts a request is disabled while it is loading, so a double
submit sends one request. A response that arrives after the user moved on
(a stale response) is ignored: cancel the request or compare a request id.

## Forms and validation
Every input has a visible label tied to it. Validate on the client for speed
and again on the server, which is the authority. Show the error next to the
field, name the field in the message, and keep what the user typed.

## API contract
Type the response and handle every documented status, including 4xx and 5xx.
Show what the API returned (price, discount, total); never recompute business
rules in the component. Treat a field the contract does not list as absent.

## Accessibility
Everything works with the keyboard alone, in a logical order, with a visible
focus. Errors and loading results are announced in an aria-live region. Text
and controls meet WCAG AA contrast. Images carry alt text, icons-only buttons a
label. Move focus to the error or the result when it appears.

## Performance
Respect a budget: ship less JavaScript, split by route, lazy-load what is
below the fold, size images, and avoid layout shift by reserving space for
loading content. Memoize only after measuring.

## Tests
Unit-test logic and hooks, component-test the screen with Testing Library
(query by role and label, drive it like a user), and cover each critical flow
end to end with Playwright. Each acceptance criterion and UI state needs a test.
