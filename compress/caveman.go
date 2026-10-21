package compress

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Level selects how aggressively Compress rewrites prose.
type Level string

const (
	Lite  Level = "lite"
	Full  Level = "full"
	Ultra Level = "ultra"
)

// ReplyDirective asks the model to answer in the same terse style.
const ReplyDirective = "Reply tersely: no filler, no pleasantries, keep code and numbers exact."

// Lexicon holds the word tables of one language (lower-case words).
// Stopwords only detect the language; Articles, Fillers and Hedges are removed.
type Lexicon struct {
	Lang      string
	Stopwords []string
	Articles  []string
	Fillers   []string
	Hedges    []string
}

type cvTable struct {
	stop, articles, fillers, hedges map[string]bool
}

var (
	cvMu     sync.RWMutex
	cvTables = map[string]*cvTable{}
)

func init() {
	for _, l := range cvBuiltin {
		RegisterLexicon(l)
	}
}

// RegisterLexicon adds a language table, replacing any table with the same Lang.
func RegisterLexicon(l Lexicon) {
	t := &cvTable{
		stop:     cvSet(l.Stopwords),
		articles: cvSet(l.Articles),
		fillers:  cvSet(l.Fillers),
		hedges:   cvSet(l.Hedges),
	}
	cvMu.Lock()
	cvTables[strings.ToLower(l.Lang)] = t
	cvMu.Unlock()
}

func cvSet(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			m[w] = true
		}
	}
	return m
}

// Compress rewrites text at the given level. Code, URLs, paths, quoted
// strings and JSON-looking lines are kept byte-identical. The result is never
// longer than the input, and an empty result returns the input.
func Compress(text string, lvl Level) string {
	if text == "" {
		return text
	}
	out := cvRun(text, lvl, cvDetect(text))
	if out == "" || len(out) > len(text) {
		return text
	}
	return out
}

// cvRun processes line by line: fenced blocks and JSON-looking lines are
// copied verbatim, prose lines are rewritten, and duplicate or repeated blank
// prose lines are dropped.
func cvRun(text string, lvl Level, t *cvTable) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	var prot []bool
	fence := ""
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		isProt := false
		switch {
		case fence != "":
			isProt = true
			if cvCloses(trim, fence) {
				fence = ""
			}
		case cvFenceOpen(trim) != "":
			isProt = true
			fence = cvFenceOpen(trim)
		case cvJSONLike(trim), cvTableRow(trim), cvCodeLike(trim):
			// JSON, Markdown table rows and code-like lines are copied verbatim.
			isProt = true
		case strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t"):
			// Indented code: copied verbatim so words inside are never dropped.
			isProt = trim != ""
		}
		s := line
		if !isProt {
			s = ""
			if trim != "" {
				s = cvProse(line, lvl, t)
			}
		}
		if n := len(out); !isProt && n > 0 && !prot[n-1] && out[n-1] == s {
			continue
		}
		out = append(out, s)
		prot = append(prot, isProt)
	}
	return strings.Join(out, "\n")
}

func cvFenceOpen(trim string) string {
	if len(trim) < 3 || (trim[0] != '`' && trim[0] != '~') {
		return ""
	}
	n := 1
	for n < len(trim) && trim[n] == trim[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return trim[:n]
}

func cvCloses(trim, fence string) bool {
	c := fence[0]
	n := 0
	for n < len(trim) && trim[n] == c {
		n++
	}
	return n >= len(fence) && n == len(trim)
}

// cvTableRow reports a Markdown table row: its first non-space character is '|'.
func cvTableRow(trim string) bool {
	return strings.HasPrefix(trim, "|")
}

// cvCodeOps are operators and brackets that mark a line as code. Prose with
// one of them is copied verbatim too, which is the safe side of the trade.
var cvCodeOps = []string{"==", "!=", ":=", "=>", "->", "&&", "||", "{", "}"}

// cvCodeLike reports a line that looks like code: it contains a code operator
// or brace, or ends with ';' or ') {'.
func cvCodeLike(trim string) bool {
	for _, op := range cvCodeOps {
		if strings.Contains(trim, op) {
			return true
		}
	}
	return strings.HasSuffix(trim, ";") || strings.HasSuffix(trim, ") {") || strings.HasSuffix(trim, "){")
}

func cvJSONLike(trim string) bool {
	return strings.HasPrefix(trim, "{") && strings.HasSuffix(trim, "}") ||
		strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]")
}

// cvDetect returns the table of the only language whose stopwords occur most
// often, or nil when there is no clear winner or the text has under 5 words.
func cvDetect(text string) *cvTable {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !isWordRune(r) })
	if len(words) < 5 {
		return nil
	}
	cvMu.RLock()
	defer cvMu.RUnlock()
	var best *cvTable
	bestN, ties := 0, 0
	for _, t := range cvTables {
		n := 0
		for _, w := range words {
			if t.stop[w] {
				n++
			}
		}
		switch {
		case n > bestN:
			best, bestN, ties = t, n, 0
		case n == bestN && n > 0:
			ties++
		}
	}
	if best == nil || ties > 0 {
		return nil
	}
	return best
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

type cvKind int

const (
	cvWord cvKind = iota
	cvSpace
	cvPunct
	cvProt
)

type cvTok struct {
	s    string
	kind cvKind
}

type cvSpan struct {
	s    string
	prot bool
}

// cvSplit separates protected spans (inline code, quoted strings, URLs and
// path-like tokens) from prose. Only ASCII delimiters are matched, so byte
// indexing is UTF-8 safe.
func cvSplit(line string) []cvSpan {
	var spans []cvSpan
	start := 0
	flush := func(end int) {
		if end > start {
			spans = append(spans, cvSpan{line[start:end], false})
		}
	}
	mark := func(from, to int) {
		flush(from)
		spans = append(spans, cvSpan{line[from:to], true})
		start = to
	}
	for i := 0; i < len(line); {
		rest := line[i:]
		if q := cvSingleQuote(line, i); q > 0 {
			mark(i, i+q)
			i += q
			continue
		}
		adv := 0
		switch {
		case line[i] == '`':
			if j := strings.IndexByte(rest[1:], '`'); j >= 0 {
				adv = j + 2
			}
		case line[i] == '"':
			if j := strings.IndexByte(rest[1:], '"'); j >= 0 {
				adv = j + 2
			}
		case strings.HasPrefix(rest, "“"):
			if j := strings.Index(rest, "”"); j >= 0 {
				adv = j + len("”")
			}
		case i == 0 || line[i-1] == ' ' || line[i-1] == '\t':
			end := len(line)
			if k := strings.IndexAny(rest, " \t"); k >= 0 {
				end = i + k
			}
			tok := line[i:end]
			if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") || strings.ContainsAny(tok, "/\\") {
				adv = end - i
			}
		}
		if adv > 0 {
			mark(i, i+adv)
			i += adv
			continue
		}
		i++
	}
	flush(len(line))
	return spans
}

// cvSingleQuote returns the byte length of a single-quoted string that starts
// at i, or 0. The opening quote must follow the start of the line, a space, an
// open bracket, '=', ':' or ','. The closing quote must be followed by the end
// of the line, a space, a closing bracket or punctuation, so apostrophes such
// as "don't" are not treated as quotes.
func cvSingleQuote(line string, i int) int {
	if line[i] != '\'' || (i > 0 && strings.IndexByte(" \t([{=:,", line[i-1]) < 0) {
		return 0
	}
	for j := i + 1; j < len(line); j++ {
		if line[j] != '\'' {
			continue
		}
		if j+1 == len(line) || strings.IndexByte(" \t)]}.,;:!?", line[j+1]) >= 0 {
			return j - i + 1
		}
	}
	return 0
}

// cvTokenize splits prose into word, space and single-rune punctuation tokens.
func cvTokenize(s string, out []cvTok) []cvTok {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		j := i + n
		switch {
		case unicode.IsSpace(r):
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if !unicode.IsSpace(r2) {
					break
				}
				j += n2
			}
			out = append(out, cvTok{" ", cvSpace})
		case isWordRune(r):
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if !isWordRune(r2) {
					break
				}
				j += n2
			}
			out = append(out, cvTok{s[i:j], cvWord})
		default:
			out = append(out, cvTok{s[i:j], cvPunct})
		}
		i = j
	}
	return out
}

// cvProse applies the mechanical rules, then word removal for the level.
func cvProse(line string, lvl Level, t *cvTable) string {
	lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	var toks []cvTok
	for _, sp := range cvSplit(line[len(lead):]) {
		if sp.prot {
			toks = append(toks, cvTok{sp.s, cvProt})
		} else {
			toks = cvTokenize(sp.s, toks)
		}
	}

	// Mechanical: collapse spaces, and runs of three or more '!' or '?' into
	// one. Pairs and other punctuation are kept (code uses "::", "??", ";;").
	norm := make([]cvTok, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		tk := toks[i]
		n := len(norm)
		if tk.kind == cvSpace && n > 0 && norm[n-1].kind == cvSpace {
			continue
		}
		if tk.kind == cvPunct && (tk.s == "!" || tk.s == "?") {
			j := i
			for j+1 < len(toks) && toks[j+1].kind == cvPunct && toks[j+1].s == tk.s {
				j++
			}
			if j-i >= 2 {
				norm = append(norm, tk)
				i = j
				continue
			}
		}
		norm = append(norm, tk)
	}

	if t != nil && lvl != "" {
		norm = cvDropWords(norm, cvSentenceStarts(norm), lvl, t)
	}

	for len(norm) > 0 && norm[0].kind == cvSpace {
		norm = norm[1:]
	}
	for len(norm) > 0 && norm[len(norm)-1].kind == cvSpace {
		norm = norm[:len(norm)-1]
	}
	var b strings.Builder
	b.WriteString(lead)
	for _, tk := range norm {
		b.WriteString(tk.s)
	}
	return b.String()
}

// cvSentenceStarts marks word tokens that begin a sentence.
func cvSentenceStarts(toks []cvTok) []bool {
	starts := make([]bool, len(toks))
	atStart := true
	for i, tk := range toks {
		switch tk.kind {
		case cvWord:
			starts[i] = atStart
			atStart = false
		case cvProt:
			atStart = false
		case cvPunct:
			if strings.Contains(".!?", tk.s) {
				atStart = true
			}
		}
	}
	return starts
}

func cvDrop(w string, start bool, lvl Level, t *cvTable) bool {
	w = strings.ToLower(w)
	switch lvl {
	case Lite:
		return t.fillers[w] && !start
	case Full:
		return (t.fillers[w] && !start) || t.articles[w] || t.hedges[w]
	case Ultra:
		return t.fillers[w] || t.articles[w] || t.hedges[w]
	}
	return false
}

// cvDropWords removes words listed for the level together with the
// punctuation or space they leave behind. A removed sentence-initial word
// capitalises the next word.
func cvDropWords(toks []cvTok, starts []bool, lvl Level, t *cvTable) []cvTok {
	out := make([]cvTok, 0, len(toks))
	capNext := false
	for i := 0; i < len(toks); i++ {
		tk := toks[i]
		if tk.kind == cvWord && cvDrop(tk.s, starts[i], lvl, t) {
			j := i + 1
			switch {
			case starts[i]:
				for j < len(toks) && toks[j].kind == cvPunct && strings.Contains(".!?,;:", toks[j].s) {
					j++
				}
				if j < len(toks) && toks[j].kind == cvSpace {
					j++
				}
				capNext = true
			case j < len(toks) && toks[j].kind == cvPunct && strings.Contains(",;:", toks[j].s):
				j++
				if j < len(toks) && toks[j].kind == cvSpace {
					j++
				}
			case j < len(toks) && toks[j].kind == cvSpace:
				j++
			default:
				if n := len(out); n > 0 && out[n-1].kind == cvSpace {
					out = out[:n-1]
				}
			}
			i = j - 1
			continue
		}
		if tk.kind == cvWord && capNext {
			r, n := utf8.DecodeRuneInString(tk.s)
			tk.s = string(unicode.ToUpper(r)) + tk.s[n:]
			capNext = false
		}
		if tk.kind == cvProt {
			capNext = false
		}
		out = append(out, tk)
	}
	return out
}

var cvBuiltin = []Lexicon{
	{
		Lang:      "en",
		Stopwords: []string{"the", "and", "is", "are", "of", "to", "in", "that", "it", "for", "with", "you", "this", "i", "be", "on", "not", "was", "have", "but", "or", "from", "by", "your", "we", "they", "can", "will", "if"},
		Articles:  []string{"a", "an", "the"},
		Fillers:   []string{"basically", "actually", "just", "really", "simply", "certainly", "literally", "honestly", "obviously", "sure", "absolutely", "definitely"},
		Hedges:    []string{"maybe", "perhaps", "probably", "possibly", "somewhat", "arguably", "seemingly"},
	},
	{
		Lang:      "pt",
		Stopwords: []string{"de", "que", "não", "uma", "para", "com", "os", "em", "um", "é", "do", "da", "dos", "das", "eu", "você", "mas", "como", "muito", "isso"},
		Articles:  []string{"um", "uma", "uns", "umas", "os"},
		Fillers:   []string{"basicamente", "realmente", "simplesmente", "claro", "certamente", "literalmente", "obviamente"},
		Hedges:    []string{"talvez", "provavelmente", "possivelmente"},
	},
	{
		Lang:      "es",
		Stopwords: []string{"de", "que", "la", "el", "los", "las", "y", "en", "es", "por", "con", "para", "una", "un", "no", "se", "lo", "del", "al", "pero", "como"},
		Articles:  []string{"el", "los", "las", "un", "una", "unos", "unas"},
		Fillers:   []string{"básicamente", "realmente", "simplemente", "ciertamente", "literalmente", "obviamente"},
		Hedges:    []string{"quizás", "probablemente", "posiblemente"},
	},
	{
		Lang:      "fr",
		Stopwords: []string{"le", "la", "les", "et", "est", "une", "des", "pour", "que", "dans", "pas", "je", "tu", "du", "de", "il", "elle", "ce", "qui", "sur", "avec", "nous", "vous", "ne"},
		Articles:  []string{"le", "la", "les", "un", "une", "des"},
		Fillers:   []string{"vraiment", "simplement", "juste", "réellement", "évidemment", "absolument"},
		Hedges:    []string{"probablement", "possiblement"},
	},
	{
		Lang:      "de",
		Stopwords: []string{"der", "die", "das", "und", "ist", "nicht", "ein", "eine", "zu", "mit", "sie", "auf", "für", "du", "ich", "im", "den", "dem", "von", "wir", "ihr", "auch", "aber"},
		Articles:  []string{"der", "die", "das", "ein", "eine"},
		Fillers:   []string{"einfach", "eigentlich", "wirklich", "tatsächlich", "natürlich", "sicherlich"},
		Hedges:    []string{"vielleicht", "wahrscheinlich", "möglicherweise"},
	},
	{
		Lang:      "it",
		Stopwords: []string{"il", "di", "che", "e", "per", "non", "una", "sono", "della", "gli", "con", "è", "lo", "nella", "questo", "anche", "come", "ma", "i"},
		Articles:  []string{"il", "lo", "i", "gli", "uno", "una", "un"},
		Fillers:   []string{"davvero", "semplicemente", "certamente", "ovviamente", "assolutamente", "praticamente"},
		Hedges:    []string{"forse", "probabilmente", "possibilmente"},
	},
}
