package compress

import (
	"strings"
	"testing"
)

func TestCompressKeepsCode(t *testing.T) {
	fence := "```go\nfunc  main() {\n\n\n}\n```"
	cases := map[string]struct {
		in      string
		protect []string
	}{
		"en": {
			in:      "Sure! I think you should basically just run the tests with `go test ./...` and check https://example.com/docs/a?b=1 in src/main.go. Error: \"permission denied: /etc/shadow\"\n" + fence + "\n",
			protect: []string{"`go test ./...`", "https://example.com/docs/a?b=1", "src/main.go", "\"permission denied: /etc/shadow\"", fence},
		},
		"pt": {
			in:      "Claro! Eu acho que você deve basicamente rodar os testes com `go test ./...` em src/main.go. Erro: \"arquivo não encontrado\"\n" + fence + "\n",
			protect: []string{"`go test ./...`", "src/main.go", "\"arquivo não encontrado\"", fence},
		},
		"ja": {
			in:      "はい、テストを実行してください `go test ./...` https://example.com/x 。 エラー: \"権限がありません\"\n" + fence + "\n",
			protect: []string{"`go test ./...`", "https://example.com/x", "\"権限がありません\"", fence},
		},
	}
	for name, c := range cases {
		for _, lvl := range []Level{Lite, Full, Ultra} {
			out := Compress(c.in, lvl)
			for _, p := range c.protect {
				if !strings.Contains(out, p) {
					t.Errorf("%s/%s: protected %q lost\nout: %q", name, lvl, p, out)
				}
			}
		}
	}
}

func TestCompressDropsFillerEn(t *testing.T) {
	in := "Sure! I think you should basically just run the tests"
	out := Compress(in, Full)
	if len(out) >= len(in) {
		t.Errorf("not shorter: %q", out)
	}
	if !strings.Contains(out, "run") || !strings.Contains(out, "tests") {
		t.Errorf("lost content: %q", out)
	}
}

func TestCompressPortuguese(t *testing.T) {
	out := Compress("Claro! Eu acho que você deve basicamente rodar os testes agora.", Full)
	if strings.Contains(out, " os ") || strings.Contains(out, "basicamente") {
		t.Errorf("article or filler kept: %q", out)
	}
	keep := Compress("Eu vou a praça com os amigos hoje.", Full)
	if !strings.Contains(keep, " a praça") {
		t.Errorf("preposition 'a' removed: %q", keep)
	}
}

func TestCompressSpanish(t *testing.T) {
	out := Compress("Bueno, yo creo que debes simplemente ejecutar las pruebas de los datos de la tabla.", Full)
	if strings.Contains(out, " las ") || strings.Contains(out, " los ") || strings.Contains(out, "simplemente") {
		t.Errorf("article or filler kept: %q", out)
	}
	keep := Compress("Voy a la casa de mi madre con los niños.", Full)
	if !strings.Contains(keep, " a la casa") {
		t.Errorf("preposition 'a' removed: %q", keep)
	}
}

func TestCompressFrenchGermanItalian(t *testing.T) {
	cases := map[string]string{
		"fr": "Bien sûr, je pense que tu dois simplement lancer les tests dans le dossier.",
		"de": "Natürlich glaube ich, du solltest einfach die Tests im Ordner ausführen.",
		"it": "Certo, penso che tu debba semplicemente eseguire i test nella cartella.",
	}
	for lang, in := range cases {
		out := Compress(in, Full)
		if out == "" || len(out) >= len(in) {
			t.Errorf("%s: not shorter: %q", lang, out)
		}
	}
}

func TestCompressUnknownLanguageMechanicalOnly(t *testing.T) {
	cases := []string{
		"こんにちは  世界  \n\n\n\nテスト  \n",
		"مرحبا   بالعالم\n\n\nاختبار  \n",
	}
	for _, in := range cases {
		out := Compress(in, Ultra)
		if strings.Join(strings.Fields(in), "") != strings.Join(strings.Fields(out), "") {
			t.Errorf("letters changed: in %q out %q", in, out)
		}
		if strings.Contains(out, "  ") || strings.Contains(out, " \n") {
			t.Errorf("whitespace not normalised: %q", out)
		}
	}
}

func TestRegisterLexicon(t *testing.T) {
	RegisterLexicon(Lexicon{
		Lang:      "nl",
		Stopwords: []string{"ik", "denk", "dat", "het", "een", "niet", "je", "op", "voor", "met", "maar", "ook"},
		Articles:  []string{"de", "het", "een"},
	})
	out := Compress("Ik denk dat de server draait op de nieuwe machine.", Full)
	if strings.Contains(out, " de ") {
		t.Errorf("Dutch article kept: %q", out)
	}
}

func TestCompressLevels(t *testing.T) {
	in := "Sure! I think you should basically just run the tests.\n\nActually, the server is perhaps down.\n\n\nMaybe check the logs now!!!"
	lite := Compress(in, Lite)
	full := Compress(in, Full)
	ultra := Compress(in, Ultra)
	if !(len(full) <= len(lite) && len(lite) <= len(in)) {
		t.Errorf("want full <= lite <= input, got %d %d %d", len(full), len(lite), len(in))
	}
	if len(ultra) > len(full) {
		t.Errorf("want ultra <= full, got %d > %d", len(ultra), len(full))
	}
}

func TestCompressIdempotent(t *testing.T) {
	inputs := []string{
		"Sure! I think you should basically just run the tests",
		"Done. Sure, next step is the deploy.\n\n\nAlso, maybe check the logs!!!",
		"Claro! Eu acho que você deve basicamente rodar os testes agora.",
		"```\nx  y\n```\nSure thing, the server is down probably.",
	}
	for _, in := range inputs {
		for _, lvl := range []Level{Lite, Full, Ultra} {
			once := Compress(in, lvl)
			twice := Compress(once, lvl)
			if once != twice {
				t.Errorf("%s not idempotent: %q -> %q", lvl, once, twice)
			}
		}
	}
}

func TestCompressNeverGrowsOrEmpties(t *testing.T) {
	cases := []string{
		"",
		"```\ncode only\n```",
		"```\nunterminated\nx",
		"a\r\nb\r\n",
		"   \n  ",
		"hello",
		"Sure. Actually. Basically. Just. Really.",
		"!!!",
	}
	for _, in := range cases {
		for _, lvl := range []Level{Lite, Full, Ultra} {
			out := Compress(in, lvl)
			if len(out) > len(in) {
				t.Errorf("%q grew to %q", in, out)
			}
			if in != "" && out == "" {
				t.Errorf("%q emptied", in)
			}
		}
	}
}

func TestCompressKeepsCodePunctuation(t *testing.T) {
	// Level "" runs only the mechanical rules, so no words are dropped.
	cases := map[string]string{
		"std::vector<int> v;":     "std::vector<int> v;",
		"a ?? b":                  "a ?? b",
		"x ;; y :: z ,, w":        "x ;; y :: z ,, w",
		"Done!!":                  "Done!!",
		"Really???":               "Really?",
		"Done!!!!":                "Done!",
		"Wait!!!\nnext ???? line": "Wait!\nnext ? line",
	}
	for in, want := range cases {
		if got := Compress(in, Level("")); got != want {
			t.Errorf("Compress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompressKeepsSingleQuotedStrings(t *testing.T) {
	in := "The error is 'the file is not found' today"
	for _, lvl := range []Level{Lite, Full, Ultra} {
		out := Compress(in, lvl)
		if !strings.Contains(out, "'the file is not found'") {
			t.Errorf("%s: quoted string changed: %q", lvl, out)
		}
	}
	// An apostrophe is not an opening quote, so the article after it still goes.
	if out := Compress("I don't think the file is here", Full); !strings.Contains(out, "don't") || strings.Contains(out, " the ") {
		t.Errorf("apostrophe handled as quote: %q", out)
	}
}

func TestCompressKeepsIndentedCode(t *testing.T) {
	in := "Look at this:\n    the value is set\n\tthe other value\nSure, the end."
	for _, lvl := range []Level{Lite, Full, Ultra} {
		out := Compress(in, lvl)
		if !strings.Contains(out, "    the value is set") || !strings.Contains(out, "\tthe other value") {
			t.Errorf("%s: indented code changed: %q", lvl, out)
		}
	}
}

func TestCompressKeepsTableRowsAndCodeLines(t *testing.T) {
	keep := []string{
		"if a == b {",
		"x := y && z",
		"a != b",
		"f => g",
		"p -> q",
		"x || y",
		"func f() {",
		"return f(x);",
		"| a | b |",
		"  | the | is |",
	}
	for _, line := range keep {
		for _, lvl := range []Level{Lite, Full, Ultra} {
			if out := Compress(line, lvl); out != line {
				t.Errorf("%s: %q changed to %q", lvl, line, out)
			}
		}
	}
	// Protected lines survive next to prose, byte for byte.
	in := "The answer is basically in the table below.\n| a | b |\nif a == b { return the }"
	out := Compress(in, Ultra)
	if !strings.Contains(out, "| a | b |\nif a == b { return the }") {
		t.Errorf("code or table lines altered: %q", out)
	}
}

func TestCompressStillCompressesProseWithPunctuation(t *testing.T) {
	in := "I think this is basically the answer, honestly."
	out := Compress(in, Full)
	if out == in || strings.Contains(strings.ToLower(out), "basically") {
		t.Errorf("prose not compressed: %q", out)
	}
}
