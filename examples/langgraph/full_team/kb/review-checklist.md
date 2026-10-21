# Code review checklist

1. Does the change satisfy the acceptance criteria and architect constraints it cites?
2. Is every check-then-act sequence atomic?
3. Are retried operations idempotent?
4. Is every external input validated, and every error handled?
5. Are there tests for the happy path, the failure path and the race?
6. Return the task for any correctness or security defect; list style remarks without blocking.
