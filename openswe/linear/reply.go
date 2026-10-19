package linear

import (
	"context"
	"errors"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

// Notifier wraps the agent runner and comments the outcome of each run on the
// Linear issue it came from. Install it as the KindAgent runner of the
// Dispatcher, like slack.Notifier.
type Notifier struct {
	Client *Client
	Inner  triggers.Runner
	// Describe builds the comment body from the run's final answer (empty
	// when the runner set none) and its error. The default posts the answer,
	// or says the run failed. Use it to add the PR link.
	Describe func(ctx context.Context, t triggers.Task, final string, runErr error) string
}

// Run implements triggers.Runner.
func (n Notifier) Run(ctx context.Context, t triggers.Task) error {
	ctx, final := triggers.WithResultSink(ctx)
	err := n.Inner.Run(ctx, t)
	issue, _ := t.Metadata["linear_issue_id"].(string)
	if issue == "" || n.Client == nil {
		return err
	}
	text := "Run finished."
	switch {
	case err != nil:
		text = fmt.Sprintf("The run failed: %v", err)
	case final() != "":
		text = final()
	}
	if n.Describe != nil {
		text = n.Describe(ctx, t, final(), err)
	}
	if _, perr := n.Client.CreateComment(ctx, issue, text); perr != nil {
		return errors.Join(err, perr)
	}
	return err
}
