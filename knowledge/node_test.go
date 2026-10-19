package knowledge

import (
	"context"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func TestNode(t *testing.T) {
	n := Node(sourced{}, "pergunta", "resposta")
	out, err := n(context.Background(), graph.State{"pergunta": "q?"})
	if err != nil {
		t.Fatal(err)
	}
	st := out.(graph.State)
	if st["resposta"] != "resposta [1]" {
		t.Errorf("answer %v", st["resposta"])
	}
	if srcs, ok := st["resposta_sources"].([]Source); !ok || len(srcs) != 1 || srcs[0].ID != "doc-1" {
		t.Errorf("sources %#v", st["resposta_sources"])
	}
	if _, err := n(context.Background(), graph.State{"pergunta": 42}); err == nil {
		t.Error("non-string question must fail")
	}
}
