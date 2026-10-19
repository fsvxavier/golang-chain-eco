package knowledge

import (
	"context"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// Node returns a graph node that answers the question stored under
// questionKey and writes the text to answerKey and the sources to
// answerKey+"_sources".
func Node(a Answerer, questionKey, answerKey string) graph.NodeFunc {
	return func(ctx context.Context, state graph.State) (any, error) {
		q, ok := state[questionKey].(string)
		if !ok {
			return nil, fmt.Errorf("knowledge: state %q must be a string", questionKey)
		}
		ans, err := a.Answer(ctx, Question{Text: q})
		if err != nil {
			return nil, err
		}
		return graph.State{answerKey: ans.Text, answerKey + "_sources": ans.Sources}, nil
	}
}
