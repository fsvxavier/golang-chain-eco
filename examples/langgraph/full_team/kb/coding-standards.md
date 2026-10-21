# Coding standards

## Errors
Wrap errors with context using fmt.Errorf and %w. Never ignore an error from a
payment, coupon or database call.

## Concurrency
Protect shared state with one lock or one atomic compare-and-swap. Check and
update in a single step: a read followed by a write is a race.

## Idempotency
Every operation that can be retried (confirm, release, webhook) must be safe to
run twice. Key it by an idempotency key stored with the result.

## Tests
Each acceptance criterion needs a test. Concurrency code needs a test that runs
the operation from many goroutines and checks the invariant.
