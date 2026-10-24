# QA checklist

1. For each acceptance criterion and UI state (loading, empty, error), name the test that observes it and say whether it passed.
2. Probe empty input, maximum input, invalid input, a repeated click or double submit, and a stale response that arrives late.
3. Probe a failing or slow request, a rejected response (4xx, 5xx) and an expired or unknown value; the error must be announced in an aria-live region.
4. Probe the keyboard alone (tab order, Enter and Escape, visible focus, focus moved to the error), a narrow viewport and zoom.
5. Check labels, roles, alt text and contrast, and that nothing is injected as raw HTML.
6. Trace every test to a criterion with a TRACE line; a defect report states the input, the expected result and the actual result.
7. Return the task on any failed criterion or any defect that loses the user's input, blocks the keyboard or hides an error.
