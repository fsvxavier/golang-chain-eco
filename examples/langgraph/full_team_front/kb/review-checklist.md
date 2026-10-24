# Code review checklist

1. Does the change satisfy the acceptance criteria, UI states and constraints it cites?
2. Does every screen handle loading, empty and error, and can a stale or double response corrupt state?
3. Are controls semantic, labelled, keyboard-operable and visibly focused, with errors announced in an aria-live region?
4. Is every external input validated, every request error handled, and nothing injected as raw HTML or exposed as a secret in client code?
5. Does the component show what the API returned instead of recomputing business rules, and handle every documented status?
6. Is the performance budget respected (bundle size, lazy loading, no layout shift, no needless re-renders)?
7. Do the unit tests drive the component by role and label and cover the happy path and the failure states?
8. Return the task for any correctness, accessibility or security defect; list style remarks without blocking.
