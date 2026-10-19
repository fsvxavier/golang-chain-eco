package babysit

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/fsvxavier/golang-chain-eco/core/runnables"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// Skill is the baby-sit skill text (SKILL.md) for the agent's skills
// directory or prompt.
//
//go:embed skill.md
var Skill string

type manageArgs struct {
	Action string `json:"action" jsonschema:"enum=start|stop|status,description=start or stop a watch, or read its status"`
	PRURL  string `json:"pr_url" description:"Canonical https://github.com/<owner>/<repo>/pull/<number> URL"`
}

type rerunArgs struct {
	PRURL    string `json:"pr_url"`
	HeadSHA  string `json:"head_sha" description:"The verified current head SHA of the PR"`
	Check    string `json:"check_name" description:"Name of the failing GitHub Actions check"`
	Evidence string `json:"evidence" description:"Concise evidence the failure is flaky or transient"`
}

// Tools returns manage_baby_sit and rerun_flaky_check. The owning thread is
// taken from the run's Configurable["thread_id"].
func (w *Watcher) Tools() []tools.Tool {
	manage := tools.New("manage_baby_sit",
		"Start, stop or inspect the durable CI watch of a pull request. Start only after confirming checks are pending or failing; the watch wakes you on failures and when everything is green.",
		func(ctx context.Context, a manageArgs) (any, error) {
			k, err := github.ParsePRURL(a.PRURL)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			switch a.Action {
			case "start":
				thread, _ := runnables.GetConfig(ctx).Configurable["thread_id"].(string)
				if thread == "" {
					return nil, tools.Errorf("a watch needs a thread to wake: run inside a thread")
				}
				res, err := w.Start(ctx, k, thread)
				if err != nil {
					return nil, tools.Errorf("%v", err)
				}
				if !res.Active {
					return res.Reason, nil
				}
				return fmt.Sprintf("watching %s on head %s (%s; %d checks)", k, short(res.Watch.HeadSHA), res.Summary.State, res.Summary.Total), nil
			case "stop":
				wt, err := w.Stop(ctx, k, "stopped on request")
				if err != nil {
					return nil, tools.Errorf("%v", err)
				}
				if wt == nil {
					return "no watch was active on " + k.String(), nil
				}
				return "stopped watching " + k.String(), nil
			case "status":
				wt, err := w.Get(ctx, k)
				if err != nil {
					return nil, tools.Errorf("%v", err)
				}
				if wt == nil {
					return "no watch on " + k.String(), nil
				}
				return fmt.Sprintf("active=%v head=%s retries=%d/%d %s", wt.Active, short(wt.HeadSHA), wt.Retries, MaxRetriesPerHead, wt.StoppedFor), nil
			}
			return nil, tools.Errorf("unknown action %q", a.Action)
		})
	rerun := tools.New("rerun_flaky_check",
		"Re-run only the failed jobs of a failing GitHub Actions check you have evidence is flaky. At most 3 per head. Refuses non-Actions checks and stale heads.",
		func(ctx context.Context, a rerunArgs) (any, error) {
			k, err := github.ParsePRURL(a.PRURL)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			if a.Evidence == "" {
				return nil, tools.Errorf("evidence is required: say why this failure is flaky")
			}
			wt, err := w.RerunFlaky(ctx, k, a.HeadSHA, a.Check, a.Evidence)
			if err != nil {
				return nil, tools.Errorf("%v", err)
			}
			return fmt.Sprintf("re-ran failed jobs of %q; flaky re-runs for this head: %d/%d", a.Check, wt.Retries, MaxRetriesPerHead), nil
		})
	return []tools.Tool{manage, rerun}
}
