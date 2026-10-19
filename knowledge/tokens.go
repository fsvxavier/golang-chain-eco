package knowledge

import "unicode/utf8"

// TokenCounter estimates the number of tokens in a text.
type TokenCounter interface {
	Count(text string) int
}

// Heuristic counts tokens as ceil(runes / CharsPerToken).
// ponytail: character heuristic, not a real tokenizer; swap in a model-specific counter if budgets get tight.
type Heuristic struct {
	CharsPerToken int
}

// Count implements TokenCounter. A zero CharsPerToken means 4.
func (h Heuristic) Count(text string) int {
	n := utf8.RuneCountInString(text)
	per := h.CharsPerToken
	if per <= 0 {
		per = 4
	}
	return (n + per - 1) / per
}
