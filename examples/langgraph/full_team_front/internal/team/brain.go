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
	if strings.Contains(prompt, "ROLL CALL:") {
		return rollCall(field(prompt, "WORKER:"))
	}
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
		return "SPEC:\nSTORY: As a shopper, I want to enter a coupon code on the checkout screen, so that I see my discounted total before paying.\n" +
			"AC1: Applying a valid code shows the discount line and the new total without leaving the screen.\n" +
			"AC2: An expired or unknown code shows an error message under the field, announced to screen readers through an aria-live region, and the total stays unchanged.\n" +
			"AC3: While the code is checked the field and button are disabled, so a double submit sends one request and a stale response is ignored.\n" +
			"AC4: The field is keyboard operable: Enter applies the code, and on failure focus moves to the error message and stays visible.\n" +
			"UI_STATES: loading while the code is checked; empty before a code is entered; error with the rejection message.\n" +
			"OUT_OF_SCOPE: coupon authoring UI.\nASSUMPTIONS: one coupon per order." + open
	case role == "leader" && field(prompt, "MODE:") == "review" && id == "T2" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. The hook recomputes the discount in the component: show the discount_percent and total the API returned, do not copy the pricing rules into the UI."
	case role == "leader" && field(prompt, "MODE:") == "review":
		return "VERDICT: APPROVE\nFEEDBACK: none"
	case role == "leader":
		return "TASK|T1|Coupon field component|low|Labelled input and apply button on the checkout form (AC1, AC4).\n" +
			"TASK|T2|Coupon validation hook|medium|useCoupon: look the code up, expose loading and error, ignore stale responses (AC1, AC2).\n" +
			"TASK|T3|Error announcement and double-submit guard|high|Announce the rejection in an aria-live region and block a second submit while loading (AC2, AC3).|PREFER:senior-1\n" +
			"TASK|T4|Discount line and UI states|high|Show the discount and total, with loading, empty and error states (AC1, AC2).\n" +
			"TASK|T5|Keyboard and focus|low|Enter applies the code; on failure focus moves to the error message (AC4)."
	case role == "engineer":
		notes := "NOTES: first version."
		if f := after(prompt, "## Feedback to address"); f != "" {
			notes = "NOTES: addressed the feedback: " + strings.SplitN(f, "\n", 2)[0]
		}
		return fmt.Sprintf("CODE:\n```tsx\n// %s, written by %s on attempt %s\nexport const %s = () => <div role=\"status\" aria-live=\"polite\" />;\n```\n%s",
			id, field(prompt, "WHO:"), attempt, "Task"+id, notes)
	case role == "specialist" && id == "T3" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. The error is not announced and a double submit sends two requests: put the message in an aria-live region and disable the button while loading (AC2, AC3)."
	case role == "qa" && id == "T4" && attempt == "1":
		return "VERDICT: RETURN\nFEEDBACK: 1. AC2 fails: an expired coupon shows no message. Input: code OLD5, expected the visible error \"coupon expired or exhausted\" in the aria-live region, got an empty region."
	case role == "qa":
		return "VERDICT: APPROVE\nTRACE: AC1 -> test_valid_coupon_shows_discount\nTRACE: AC2 -> test_expired_coupon_error_is_announced\n" +
			"TRACE: AC3 -> test_double_submit_sends_one_request\nTRACE: AC4 -> test_enter_applies_and_focus_moves_to_error\nFEEDBACK: none"
	case role == "specialist":
		return "VERDICT: APPROVE\nFEEDBACK: none"
	}
	return "OK"
}

// rollFlavour is each role's way of answering the roll call.
var rollFlavour = map[string]string{
	"po": "com a spec debaixo do braço", "leader": "organizando a fila",
	"junior": "de caderno aberto", "mid": "no ritmo", "senior": "sempre pronto",
	"specialist": "de lupa na mão", "qa": "testando até a chamada",
}

// rollCall answers "WORKER: <id> ROLE: <role>" like a pupil in a school roll call.
func rollCall(line string) string {
	id, role, _ := strings.Cut(line, " ROLE:")
	role = strings.TrimSpace(role)
	flavour := rollFlavour[role]
	if flavour == "" {
		flavour = "presente e acordado"
	}
	return fmt.Sprintf("Presente! %s (%s), %s", strings.TrimSpace(id), role, flavour)
}
