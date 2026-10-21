package compress

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// SummaryOptions bounds the Summary. Zero values mean 3 tail messages and
// 160 bytes of JSON-encoded text per tail entry.
type SummaryOptions struct {
	TailMessages int
	TailChars    int
}

// Summary is a small, fixed-size description of a history for deciders.
type Summary struct {
	Messages         int
	Turns            int
	Tokens           int
	LargestToolBytes int
	ToolShare        float64
	CodeShare        float64 // share of Human/AI bytes inside fenced code blocks, 0..1
	JSONToolBytes    int     // bytes of Tool contents that parse as a JSON object or array
	Lang             string
	Tail             []string
}

// summaryLangSample caps how many bytes per message are scanned for language.
// ponytail: language sniff reads the first 4 KiB of each message; add a real
// detector if mixed-language histories start misclassifying.
const summaryLangSample = 4096

// summaryWordLang maps stopwords to a language. Words that are common in two
// languages are left out so they do not vote for both.
var summaryWordLang = func() map[string]string {
	lists := map[string][]string{
		"en": {"the", "and", "is", "are", "to", "of", "that", "it", "for", "with", "this", "you", "we", "not", "be", "on", "what", "how", "but", "or"},
		"pt": {"não", "uma", "os", "do", "da", "em", "um", "para", "com", "mais", "você", "isso", "são", "meu", "minha"},
		"es": {"el", "los", "las", "una", "es", "pero", "del", "está", "muy", "también", "porque", "sin", "yo", "tu", "mi"},
	}
	m := make(map[string]string)
	for lang, words := range lists {
		for _, w := range words {
			m[w] = lang
		}
	}
	return m
}()

// Summarize builds a Summary of msgs. It reads at most a bounded prefix of
// each message, so a 5 MB message costs the same as a short one.
func Summarize(msgs []messages.Message, o SummaryOptions) Summary {
	if o.TailMessages <= 0 {
		o.TailMessages = 3
	}
	if o.TailChars <= 0 {
		o.TailChars = 160
	}

	var s Summary
	s.Messages = len(msgs)
	var total, toolBytes, chatBytes, codeBytes int
	var texts []string
	for _, m := range msgs {
		text := m.Text()
		n := len(text)
		total += n
		switch m.Role {
		case messages.RoleHuman:
			s.Turns++
			texts = append(texts, sampleText(text))
			chatBytes += n
			codeBytes += fencedBytes(text)
		case messages.RoleAI:
			texts = append(texts, sampleText(text))
			chatBytes += n
			codeBytes += fencedBytes(text)
		case messages.RoleTool:
			toolBytes += n
			if n > s.LargestToolBytes {
				s.LargestToolBytes = n
			}
			if isJSONContainer(text) {
				s.JSONToolBytes += n
			}
		}
	}
	if total > 0 {
		s.ToolShare = float64(toolBytes) / float64(total)
	}
	if chatBytes > 0 {
		s.CodeShare = float64(codeBytes) / float64(chatBytes)
	}
	s.Tokens = total / 4
	s.Lang = summaryLang(texts)

	start := len(msgs) - o.TailMessages
	if start < 0 {
		start = 0
	}
	s.Tail = []string{}
	for _, m := range msgs[start:] {
		s.Tail = append(s.Tail, clipEncoded(m.Text(), o.TailChars))
	}
	return s
}

// State returns the Summary as the JSON object deciders read.
func (s Summary) State() State {
	tail := s.Tail
	if tail == nil {
		tail = []string{}
	}
	return State{
		"messages":           s.Messages,
		"turns":              s.Turns,
		"tokens":             s.Tokens,
		"tool_share":         s.ToolShare,
		"largest_tool_bytes": s.LargestToolBytes,
		"code_share":         s.CodeShare,
		"json_tool_bytes":    s.JSONToolBytes,
		"lang":               s.Lang,
		"tail":               tail,
	}
}

// EncodedTokens estimates the size of State() as JSON, at four bytes per token.
func (s Summary) EncodedTokens() int {
	b, _ := json.Marshal(s.State())
	return len(b) / 4
}

// fencedBytes counts the bytes of lines inside Markdown fenced code blocks.
// The fence lines themselves are not counted. An unclosed fence runs to the end.
func fencedBytes(text string) int {
	n, inFence := 0, false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			n += len(line)
		}
	}
	return n
}

// isJSONContainer reports whether text is a valid JSON object or array.
func isJSONContainer(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		return false
	}
	return json.Valid([]byte(t))
}

func sampleText(text string) string {
	if len(text) > summaryLangSample {
		return text[:summaryLangSample]
	}
	return text
}

// summaryLang returns the language with more than half of the stopword hits,
// or "other" when no language has a clear majority.
func summaryLang(texts []string) string {
	counts := map[string]int{}
	total := 0
	for _, t := range texts {
		for _, w := range strings.FieldsFunc(strings.ToLower(t), summaryNotLetter) {
			if l, ok := summaryWordLang[w]; ok {
				counts[l]++
				total++
			}
		}
	}
	best, top := "other", 0
	for l, c := range counts {
		if c > top {
			best, top = l, c
		}
	}
	if top*2 > total {
		return best
	}
	return "other"
}

func summaryNotLetter(r rune) bool { return !unicode.IsLetter(r) }

// clipEncoded keeps the longest prefix of s whose JSON-encoded form (without
// the quotes) fits in budget bytes. Cutting on rune boundaries keeps the
// result valid UTF-8.
func clipEncoded(s string, budget int) string {
	used := 0
	for i, r := range s {
		b, _ := json.Marshal(string(r))
		n := len(b) - 2
		if used+n > budget {
			return s[:i]
		}
		used += n
	}
	return s
}
