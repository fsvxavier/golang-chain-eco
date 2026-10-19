package langmem

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// RunningSummary remembers the previous summarization so the next call only
// summarizes new messages (RunningSummary).
type RunningSummary struct {
	// Summary is the latest summary text.
	Summary string `json:"summary"`
	// SummarizedMessageIDs of every message already folded into it.
	SummarizedMessageIDs []string `json:"summarized_message_ids"`
	// LastSummarizedMessageID is the newest message folded in.
	LastSummarizedMessageID string `json:"last_summarized_message_id,omitempty"`
}

// SummarizeOptions configures SummarizeMessages (summarize_messages).
type SummarizeOptions struct {
	Model models.ChatModel
	// MaxTokens bounds the returned messages, summary included, and the input
	// fed to the summarizer. Required.
	MaxTokens int
	// MaxTokensBeforeSummary triggers summarization once the messages reach
	// it (default MaxTokens). Raise it to let the summarizer see more.
	MaxTokensBeforeSummary int
	// MaxSummaryTokens is the budget reserved for the summary (default 256).
	// It is not enforced on the model: bind max_tokens on Model for that.
	MaxSummaryTokens int
	// Counter defaults to messages.ApproxTokens; plug a real tokenizer.
	Counter messages.TokenCounter
	// InitialSummaryPrompt is the instruction after the messages for the first
	// summary; ExistingSummaryPrompt extends one ({existing_summary});
	// FinalSummaryPrompt renders the summary system message ({summary}).
	InitialSummaryPrompt  string
	ExistingSummaryPrompt string
	FinalSummaryPrompt    string
}

const (
	defaultInitialPrompt  = "Create a summary of the conversation above:"
	defaultExistingPrompt = "This is summary of the conversation so far: {existing_summary}\n\nExtend this summary by taking into account the new messages above:"
	defaultFinalPrompt    = "Summary of the conversation so far: {summary}"
)

// SummarizationResult is the outcome of SummarizeMessages.
type SummarizationResult struct {
	// Messages are ready to send to the model: [system?, summary?, recent...].
	Messages []messages.Message
	// Running is nil when nothing has ever been summarized.
	Running *RunningSummary
}

// SummarizeMessages keeps a conversation within a token budget by replacing
// its oldest part with a running summary. Once the messages reach
// MaxTokensBeforeSummary, everything up to that point (not the leading system
// message) is summarized, provided what remains fits MaxTokens minus the
// summary budget. Trailing tool results of a summarized tool call go with it.
// Every message needs an ID; the original history is not modified.
func SummarizeMessages(ctx context.Context, msgs []messages.Message, running *RunningSummary, o SummarizeOptions) (SummarizationResult, error) {
	count := o.Counter
	if count == nil {
		count = messages.ApproxTokens
	}
	maxSum := o.MaxSummaryTokens
	if maxSum <= 0 {
		maxSum = 256
	}
	if o.MaxTokens <= 0 {
		return SummarizationResult{}, fmt.Errorf("langmem: SummarizeOptions.MaxTokens is required")
	}
	if maxSum >= o.MaxTokens {
		return SummarizationResult{}, fmt.Errorf("langmem: MaxSummaryTokens must be less than MaxTokens")
	}
	before := o.MaxTokensBeforeSummary
	if before <= 0 {
		before = o.MaxTokens
	}

	var sys *messages.Message
	if len(msgs) > 0 && msgs[0].Role == messages.RoleSystem {
		sys, msgs = &msgs[0], msgs[1:]
	}
	withSystem := func(ms []messages.Message) []messages.Message {
		if sys == nil {
			return ms
		}
		return append([]messages.Message{*sys}, ms...)
	}
	if len(msgs) == 0 {
		return SummarizationResult{Messages: withSystem(nil), Running: running}, nil
	}

	maxRemaining := o.MaxTokens - maxSum
	maxToSummarize := o.MaxTokens
	if sys != nil {
		maxRemaining -= count([]messages.Message{*sys})
	}
	summarizedIDs := map[string]bool{}
	done := 0
	if running != nil {
		for _, id := range running.SummarizedMessageIDs {
			summarizedIDs[id] = true
		}
		maxToSummarize -= count([]messages.Message{messages.System(running.Summary)})
		for i, m := range msgs {
			if m.ID == running.LastSummarizedMessageID {
				done = i + 1
				break
			}
		}
	}

	totalTokens := count(msgs[done:])
	n, nToSummarize, idx := 0, 0, max(0, done-1)
	should := false
	toolMsgs := map[string]messages.Message{}
	for i := done; i < len(msgs); i++ {
		m := msgs[i]
		if m.ID == "" {
			return SummarizationResult{}, fmt.Errorf("langmem: messages are required to have an ID")
		}
		if summarizedIDs[m.ID] {
			return SummarizationResult{}, fmt.Errorf("langmem: message %s has already been summarized", m.ID)
		}
		if m.Role == messages.RoleTool && m.ToolCallID != "" {
			toolMsgs[m.ToolCallID] = m
		}
		n += count([]messages.Message{m})
		if n >= before && totalTokens-n <= maxRemaining && !should {
			nToSummarize, should, idx = n, true, i
		}
	}
	var toSummarize []messages.Message
	if should {
		toSummarize = slices.Clone(msgs[done : idx+1])
		if last := toSummarize[len(toSummarize)-1]; last.Role == messages.RoleAI {
			for _, tc := range last.ToolCalls {
				if tm, ok := toolMsgs[tc.ID]; ok && !slices.ContainsFunc(toSummarize, func(x messages.Message) bool { return x.ID == tm.ID }) {
					nToSummarize += count([]messages.Message{tm})
					toSummarize = append(toSummarize, tm)
				}
			}
		}
	}

	next := running
	if len(toSummarize) > 0 {
		if o.Model == nil {
			return SummarizationResult{}, fmt.Errorf("langmem: SummarizeOptions.Model is required")
		}
		feed := toSummarize
		if nToSummarize > maxToSummarize { // keep the summarizer inside its own window
			trimmed := messages.Trim(feed, messages.TrimOptions{MaxTokens: maxToSummarize, Counter: count, StartOn: messages.RoleHuman})
			if len(trimmed) > 0 {
				feed = trimmed
			}
		}
		prompt := cmp(o.InitialSummaryPrompt, defaultInitialPrompt)
		if running != nil {
			prompt = fill(cmp(o.ExistingSummaryPrompt, defaultExistingPrompt), map[string]string{"existing_summary": running.Summary})
		}
		in := append(slices.Clone(feed), messages.Human(prompt))
		out, err := o.Model.Generate(ctx, in)
		if err != nil {
			return SummarizationResult{}, fmt.Errorf("langmem: summarize: %w", err)
		}
		next = &RunningSummary{Summary: out.Text(), LastSummarizedMessageID: toSummarize[len(toSummarize)-1].ID}
		if running != nil {
			next.SummarizedMessageIDs = slices.Clone(running.SummarizedMessageIDs)
		}
		for _, m := range toSummarize {
			next.SummarizedMessageIDs = append(next.SummarizedMessageIDs, m.ID)
		}
		done += len(toSummarize)
	}

	if next == nil {
		return SummarizationResult{Messages: withSystem(msgs)}, nil
	}
	var res []messages.Message
	// a system message that already embeds the old summary is dropped
	if sys != nil && !(running != nil && strings.Contains(sys.Text(), running.Summary)) {
		res = append(res, *sys)
	}
	s := messages.System(fill(cmp(o.FinalSummaryPrompt, defaultFinalPrompt), map[string]string{"summary": next.Summary}))
	s.Name = "summary"
	res = append(res, s)
	res = append(res, msgs[done:]...)
	return SummarizationResult{Messages: res, Running: next}, nil
}

// SummarizationNode is a graph node running SummarizeMessages on state
// (SummarizationNode). By default it reads "messages" and writes the
// model-ready list to "summarized_messages", leaving the full history intact;
// the running summary lives in state["context"]["running_summary"] (declare
// the channels with SummarizationSchema).
type SummarizationNode struct {
	SummarizeOptions
	InputKey  string // default "messages"
	OutputKey string // default "summarized_messages"
}

// SummarizationSchema declares the state channels the node writes.
func SummarizationSchema() graph.Schema {
	return graph.Schema{"context": channels.LastValue(), "summarized_messages": channels.LastValue()}
}

// Node returns the graph node function.
func (n SummarizationNode) Node() graph.NodeFunc {
	return func(ctx context.Context, st graph.State) (any, error) {
		in, out := cmp(n.InputKey, "messages"), cmp(n.OutputKey, "summarized_messages")
		msgs, err := messages.Coerce(st[in])
		if err != nil {
			return nil, err
		}
		cx := map[string]any{}
		if c, ok := st["context"].(map[string]any); ok {
			cx = c
		}
		rs, err := decodeRunning(cx["running_summary"])
		if err != nil {
			return nil, err
		}
		res, err := SummarizeMessages(ctx, msgs, rs, n.SummarizeOptions)
		if err != nil {
			return nil, err
		}
		upd := graph.State{out: res.Messages}
		if res.Running != nil {
			upd["context"] = mergeCtx(cx, res.Running)
			if in == out { // overwriting the main list: replace it wholesale
				upd[out] = append([]messages.Message{messages.Remove(messages.RemoveAll)}, res.Messages...)
			}
		}
		return upd, nil
	}
}

func mergeCtx(cx map[string]any, rs *RunningSummary) map[string]any {
	m := map[string]any{}
	for k, v := range cx {
		m[k] = v
	}
	m["running_summary"] = rs
	return m
}

// decodeRunning accepts *RunningSummary or the map a checkpoint round trip
// leaves behind.
func decodeRunning(v any) (*RunningSummary, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *RunningSummary:
		return x, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	rs := new(RunningSummary)
	if err := json.Unmarshal(b, rs); err != nil {
		return nil, err
	}
	return rs, nil
}

// SummarizationMiddleware plugs SummarizationNode into agents.Create: before
// each model call it refreshes the running summary, and the model sees the
// summarized view while the stored history stays complete. Unlike
// agents/middleware.Summarization it summarizes incrementally and never
// rewrites state["messages"].
type SummarizationMiddleware struct {
	SummarizeOptions
}

func (SummarizationMiddleware) Name() string { return "langmem_summarization" }

func (SummarizationMiddleware) StateSchema() graph.Schema { return SummarizationSchema() }

func (s SummarizationMiddleware) BeforeModel(ctx context.Context, st graph.State) (any, error) {
	return SummarizationNode{SummarizeOptions: s.SummarizeOptions}.Node()(ctx, st)
}

func (SummarizationMiddleware) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	if v := req.State["summarized_messages"]; v != nil {
		if view, err := messages.Coerce(v); err == nil && len(view) > 0 {
			r := *req
			r.Messages = view
			req = &r
		}
	}
	return next(ctx, req)
}
