package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/openswe/triggers"
)

// NeutralizeBroadcast escapes the mrkdwn forms that ping people (<!channel>,
// <!here>, <@U...>) so text the agent wrote, possibly under the influence of
// untrusted input, cannot notify a whole channel. Links are left alone.
func NeutralizeBroadcast(s string) string {
	return strings.NewReplacer("<!", "&lt;!", "<@", "&lt;@").Replace(s)
}

// Notifier wraps the agent runner and posts the outcome of each run into the
// Slack thread it came from. Install it as the KindAgent runner:
//
//	Runners: map[string]triggers.Runner{triggers.KindAgent: slack.Notifier{Client: c, Inner: openswe.AgentRunner{Graph: agent}}}
//
// Tasks without a Slack channel in their metadata pass through untouched.
type Notifier struct {
	Client *Client
	Inner  triggers.Runner
	// Describe builds the reply from the run's final answer (empty when the
	// runner set none) and its error. The default posts the answer, or says
	// the run failed; use Describe to add the PR link (openswe.PRLinks).
	Describe func(ctx context.Context, t triggers.Task, final string, runErr error) string
}

// Run implements triggers.Runner.
func (n Notifier) Run(ctx context.Context, t triggers.Task) error {
	ctx, final := triggers.WithResultSink(ctx)
	err := n.Inner.Run(ctx, t)
	channel, _ := t.Metadata["slack_channel"].(string)
	if channel == "" || n.Client == nil {
		return err
	}
	root, _ := t.Metadata["slack_thread_ts"].(string)
	text := defaultDescribe(final(), err)
	if n.Describe != nil {
		text = n.Describe(ctx, t, final(), err)
	}
	if _, perr := n.Client.PostMessage(ctx, channel, NeutralizeBroadcast(text), root, nil); perr != nil {
		return errors.Join(err, perr)
	}
	return err
}

func defaultDescribe(final string, err error) string {
	switch {
	case err != nil:
		return fmt.Sprintf("The run failed: %v", err)
	case final != "":
		return final
	}
	return "Run finished."
}
