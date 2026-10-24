# Code review checklist

1. Does the change satisfy the acceptance criteria, UI states and constraints it cites?
2. Does every screen handle loading, empty and error, and can a stale or double response corrupt state?
3. Are controls semantic, labelled, keyboard-operable and visibly focused?
4. Is every external input validated, every request error handled, and nothing injected as raw HTML?
5. Do the unit tests drive the component by role and label and cover the happy path and the failure states?
6. Return the task for any correctness, accessibility or security defect; list style remarks without blocking.
