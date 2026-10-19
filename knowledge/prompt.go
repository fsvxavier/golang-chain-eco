package knowledge

import (
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// DefaultSystem asks the model to answer only from the numbered context and cite it as [n].
const DefaultSystem = `Answer the question using only the numbered context below. ` +
	`If the context does not contain the answer, say you do not know. ` +
	`Cite the sources you used inline as [n], matching their numbers.`

// Prompt builds the chat messages: a system message with the instruction and the
// numbered sources, then the history, then the human question.
func Prompt(system string, ctxDocs []Source, q Question) []messages.Message {
	var b strings.Builder
	b.WriteString(system)
	if len(ctxDocs) > 0 {
		b.WriteString("\n\nContext:\n")
		for i, s := range ctxDocs {
			fmt.Fprintf(&b, "[%d] %s\n", i+1, s.Content)
		}
	}
	msgs := make([]messages.Message, 0, len(q.History)+2)
	msgs = append(msgs, messages.System(b.String()))
	msgs = append(msgs, q.History...)
	return append(msgs, messages.Human(q.Text))
}
