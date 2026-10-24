package team

import (
	"fmt"
	"strings"
)

// brain is the scripted stand-in for the model when no API key is set. It
// answers in the formats the skills define, keyed by the ROLE line of the
// prompt, and makes the three return gates return a task once so the loop back to the
// engineer is visible: the leader returns T2, the specialist T3 (joint review) and QA T4.
func Brain(prompt string) string {
	role := field(prompt, "ROLE:")
	id, attempt := field(prompt, "TASK:"), field(prompt, "ATTEMPT:")
	switch {
	case role == "manager" && field(prompt, "MODE:") == "report":
		var shipped, blocked []string
		for _, l := range strings.Split(prompt, "\n") {
			if f := strings.Fields(l); len(f) > 1 && f[0] == "TASK" {
				if strings.HasSuffix(l, "approved") {
					shipped = append(shipped, f[1])
				} else {
					blocked = append(blocked, f[1])
				}
			}
		}
		none := func(s []string) string {
			if len(s) == 0 {
				return "none"
			}
			return strings.Join(s, ", ")
		}
		return fmt.Sprintf("REPORT:\n- Shipped: %s\n- Blocked: %s\n- Next: roll out behind a flag for 5%% of checkout traffic.", none(shipped), none(blocked))
	case role == "manager":
		return "ROUTE: product-owner\nKICKOFF: " + field(prompt, "REQUEST:")
	case role == "po":
		open := ""
		if r, u := backendNamed(prompt); len(r)+len(u) == 0 {
			open = "\nOPEN ITEMS:\n- No backend was named in the demand: endpoints, payloads and errors are unverified."
		}
		return "SPEC:\nSTORY: As a shopper, I want to apply a coupon at checkout, so that I pay the discounted price.\n" +
			"AC1: A valid coupon shows its discount in the cart without consuming it.\n" +
			"AC2: An expired or exhausted coupon is rejected with a clear message.\n" +
			"AC3: At payment the coupon is consumed exactly once, even with concurrent payments.\n" +
			"AC4: If the payment fails, the coupon becomes available again.\n" +
			"UI_STATES: loading while the coupon is checked; empty before a code is entered; error with the rejection message.\n" +
			"OUT_OF_SCOPE: coupon authoring UI.\nASSUMPTIONS: one coupon per order." + open
	case role == "leader" && field(prompt, "MODE:") == "review" && id == "T2" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. The endpoint design leaks coupon state: price the cart through the shared pricing module, not a copy of its rules."
	case role == "leader" && field(prompt, "MODE:") == "review":
		return "VERDICT: APPROVE\nFEEDBACK: none"
	case role == "leader":
		return "TASK|T1|Coupon table and states|low|Schema for coupons and their state machine (AC2).\n" +
			"TASK|T2|Cart validation endpoint|medium|Validate a code and price the cart without consuming it (AC1, AC2).\n" +
			"TASK|T3|Atomic reservation with TTL|high|Reserve the last coupon atomically, expiring on TTL (AC3 and the atomicity constraint).|PREFER:senior-1\n" +
			"TASK|T4|Confirm and release on payment|high|Idempotent confirm after payment, release on failure (AC3, AC4).\n" +
			"TASK|T5|Lookup rate limit|low|Throttle coupon lookups per client (security rule)."
	case role == "engineer":
		notes := "NOTES: first version."
		if f := after(prompt, "## Feedback to address"); f != "" {
			notes = "NOTES: addressed the feedback: " + strings.SplitN(f, "\n", 2)[0]
		}
		return fmt.Sprintf("CODE:\n```go\n// %s, written by %s on attempt %s\nfunc %s() error { return nil }\n```\n%s",
			id, field(prompt, "WHO:"), attempt, "task"+id, notes)
	case role == "specialist" && id == "T3" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. The reservation is not safe when two payments race: make the check and the decrement one atomic step (constraint: atomic consumption)."
	case role == "qa" && id == "T4" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. AC2 fails: an expired coupon returns 200 instead of a rejection. Input: code EXPIRED10, expected 422, got 200."
	case role == "qa":
		return "VERDICT: APPROVE\nTRACE: AC1 -> test_valid_coupon_shows_discount\nTRACE: AC2 -> test_expired_coupon_is_rejected\n" +
			"TRACE: AC3 -> test_coupon_consumed_once_on_payment\nTRACE: AC4 -> test_coupon_released_when_payment_fails\nFEEDBACK: none"
	case role == "specialist":
		return "VERDICT: APPROVE\nFEEDBACK: none"
	}
	return "OK"
}
