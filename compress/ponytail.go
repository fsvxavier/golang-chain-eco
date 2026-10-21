package compress

// Ponytail directives. The directive is adapted from Ponytail
// (https://github.com/DietrichGebert/ponytail, MIT licensed, by Dietrich
// Gebert); see that repository for its license text.
// Each text ends with a line starting "Ponytail:". None of them lets the
// model skip validation, error handling, security or accessibility.

const ponytailLite = "Ponytail: Write the least code that solves the task and reuse what the codebase already has. Never cut input validation, error handling, security or accessibility."

const ponytailFull = `Ponytail mode: write the least code that works. Climb this ladder and stop at the first rung that holds:
1. Does it need to exist at all? Skip speculative features and say so in one line.
2. Does this codebase already have it? Reuse it.
3. Does the standard library do it?
4. Does a native platform feature cover it?
5. Does an installed dependency already solve it? Do not add a new one.
6. Can it be one line?
7. Only then, the minimum code that works.
Lazy about the solution, never about reading. Read the affected code end to end before you pick a rung.
Never cut input validation at trust boundaries, error handling that prevents data loss, security checks, accessibility basics, or the one test that guards risky logic.
Ponytail: keep the diff small, and mark any real shortcut with a comment that names its ceiling.`

const ponytailUltra = `Ponytail mode, strict: the best code is the code never written. Before you write anything, ask whether it must exist at all. Climb this ladder and stop at the first rung that holds:
1. Does it need to exist at all? Refuse speculative features and say so in one line.
2. Does this codebase already have it? Reuse the helper, type or pattern that is already there.
3. Does the standard library do it?
4. Does a native platform feature cover it? Prefer CSS, the browser, or a database constraint over app code.
5. Does an installed dependency already solve it? Never add a new dependency for what a few lines can do.
6. Can it be one line?
7. Only then, the minimum code that works.
Lazy about the solution, never about reading. Trace the real flow through every file the change touches before you pick a rung.
Never cut input validation at trust boundaries, error handling that prevents data loss, security checks, accessibility basics, or the one test that guards risky logic.
Prefer deletion to addition, boring to clever, and the smallest right diff.
Ponytail: name every deliberate simplification and its known ceiling in a short comment, and say what you skipped in at most three lines.`

// PonytailDirective returns the rules text that nudges a coding agent toward
// less code. Lite and Ultra are shorter and longer rule sets; any other level
// returns the Full text.
func PonytailDirective(lvl Level) string {
	switch lvl {
	case Lite:
		return ponytailLite
	case Ultra:
		return ponytailUltra
	default:
		return ponytailFull
	}
}
