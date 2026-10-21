# QA checklist

1. For each acceptance criterion, state how it is observed and whether it passed.
2. Probe empty input, maximum input, repeated requests and two callers at once.
3. Probe dependency failure (payment declined) and expiry (TTL passed).
4. A defect report states input, expected result and actual result.
5. Return the task on any failed criterion or any defect that can lose money or data.
