package langmem

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// OptimizerKind selects the prompt-improvement algorithm.
type OptimizerKind string

const (
	// Gradient separates "what is wrong" (a think/critique loop ending in a
	// recommendation) from "rewrite the prompt". Most thorough, most calls.
	Gradient OptimizerKind = "gradient"
	// Metaprompt reflects (think/critique) and rewrites in one conversation.
	Metaprompt OptimizerKind = "metaprompt"
	// PromptMemory is a single call that returns the new prompt: cheapest.
	PromptMemory OptimizerKind = "prompt_memory"
)

// Prompt is a named prompt under optimization (Prompt TypedDict).
type Prompt struct {
	Name   string
	Prompt string
	// UpdateInstructions guide how the prompt may change ("make minimal
	// changes...").
	UpdateInstructions string
	// WhenToUpdate describes the failures that implicate this prompt; used by
	// the multi-prompt credit assignment.
	WhenToUpdate string
}

// Trajectory is a conversation plus optional feedback: a string, a map such
// as {"score": 0, "comment": "..."}, or nil.
type Trajectory struct {
	Messages []messages.Message
	Feedback any
}

// OptimizerConfig tunes PromptOptimizer.
type OptimizerConfig struct {
	// MaxReflectionSteps / MinReflectionSteps bound the think-critique loop
	// of Gradient and Metaprompt (defaults 5 and 1).
	MaxReflectionSteps int
	MinReflectionSteps int
	// GradientPrompt and Metaprompt replace the built-in instructions. They
	// use {prompt}, {update_instructions}, {trajectories} (and for the
	// gradient update step {current_prompt}, {hypotheses}, {recommendations}).
	GradientPrompt string
	Metaprompt     string
}

// PromptOptimizer improves a system prompt from conversations and feedback:
// procedural memory (create_prompt_optimizer). It returns the prompt
// unchanged when there is no evidence of failure.
type PromptOptimizer struct {
	Model  models.ChatModel
	Kind   OptimizerKind // default Gradient
	Config OptimizerConfig
}

// NewPromptOptimizer builds an optimizer of the given kind.
func NewPromptOptimizer(model models.ChatModel, kind OptimizerKind, cfg OptimizerConfig) *PromptOptimizer {
	return &PromptOptimizer{Model: model, Kind: kind, Config: cfg}
}

const defaultGradientPrompt = `You are reviewing the performance of an AI assistant in a given interaction.

## Instructions

The current prompt that was used for the session is provided below.

<current_prompt>
{prompt}
</current_prompt>

The developer provided the following instructions around when and how to update the prompt:

<update_instructions>
{update_instructions}
</update_instructions>

## Session data

Analyze the following trajectories (and any associated user feedback) (either conversations with a user or other work that was performed by the assistant):

<trajectories>
{trajectories}
</trajectories>

## Task

Analyze the conversation, including the user's request and the assistant's response, and evaluate:
1. How effectively the assistant fulfilled the user's intent.
2. Where the assistant might have deviated from user expectations or the desired outcome.
3. Specific areas (correctness, completeness, style, tone, alignment, etc.) that need improvement.

If the prompt seems to do well, then no further action is needed. We ONLY recommend updates if there is evidence of failures.
When failures occur, we want to recommend the minimal required changes to fix the problem.

Focus on actionable changes and be concrete.

1. Summarize the key successes and failures in the assistant's response.
2. Identify which failure mode(s) best describe the issues (examples: style mismatch, unclear or incomplete instructions, flawed logic or reasoning, hallucination, etc.).
3. Based on these failure modes, recommend the most suitable edit strategy. For example, consider:
   - Use synthetic few-shot examples for style or clarifying decision boundaries.
   - Use explicit instruction updates for conditionals, rules, or logic fixes.
   - Provide step-by-step reasoning guidelines for multi-step logic problems.
4. Provide detailed, concrete suggestions for how to update the prompt accordingly.

But remember, the final updated prompt should only be changed if there is evidence of poor performance, and our recommendations should be minimally invasive.
Do not recommend generic changes that aren't clearly linked to failure modes.

First think through the conversation and critique the current behavior.
If you believe the prompt needs to further adapt to the target context, provide precise recommendations.
Otherwise, mark warrants_adjustment as false and respond with 'No recommendations.'`

const defaultGradientUpdate = `You are optimizing a prompt to handle its target task more effectively.

<current_prompt>
{current_prompt}
</current_prompt>

We hypothesize the current prompt underperforms for these reasons:

<hypotheses>
{hypotheses}
</hypotheses>

Based on these hypotheses, we recommend the following adjustments:

<recommendations>
{recommendations}
</recommendations>

The developer's instructions for updating:

<update_instructions>
{update_instructions}
</update_instructions>

Respond with the updated prompt. Remember to ONLY make changes that are clearly necessary. Aim to be minimally invasive:`

const defaultMetaprompt = `You are helping an AI assistant learn by optimizing its prompt.

## Background

Below is the current prompt:

<current_prompt>
{prompt}
</current_prompt>

The developer provided these instructions regarding when/how to update:

<update_instructions>
{update_instructions}
</update_instructions>

## Session Data

Analyze the session(s) (and any user feedback) below:

<trajectories>
{trajectories}
</trajectories>

## Instructions

1. Reflect on the agent's performance on the given session(s) and identify any real failure modes (e.g., style mismatch, unclear or incomplete instructions, flawed reasoning, etc.).
2. Recommend the minimal changes necessary to address any real failures. If the prompt performs perfectly, simply respond with the original prompt without making any changes.
3. Retain any f-string variables in the existing prompt exactly as they are (e.g. {variable_name}).

IFF changes are warranted, focus on actionable edits. Be concrete. Edits should be appropriate for the identified failure modes. For example, consider synthetic few-shot examples for style or clarifying decision boundaries, or adding or modifying explicit instructions for conditionals, rules, or logic fixes; or provide step-by-step reasoning guidelines for multi-step logic problems if the model is failing to reason appropriately.`

const promptMemoryPrompt = `You are helping an AI agent improve. You can do this by changing their system prompt.

These is their current prompt:
<current_prompt>
{current_prompt}
</current_prompt>

Here are examples of various agent trajectories and associated feedback:
<data>
{data}
</data>

Here are instructions for updating the agent's prompt:

<instructions>
{instructions}
</instructions>

Based on this, return an updated prompt

You should return the full prompt, so if there's anything from before that you want to include, make sure to do that. Feel free to override or change anything that seems irrelevant. You do not need to update the prompt - if you don't want to, just return update_prompt = false and an empty string for new prompt.`

// fill replaces {name} placeholders (and only those) in a single pass, so a
// substituted value that itself contains "{x}" is never expanded again.
func fill(t string, kv map[string]string) string {
	return varRe.ReplaceAllStringFunc(t, func(m string) string {
		if v, ok := kv[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// FormatSessions renders trajectories the way the optimizers read them.
func FormatSessions(ts []Trajectory) string {
	var parts []string
	for i, t := range ts {
		id := fmt.Sprintf("%d", i+1)
		fb := ""
		switch f := t.Feedback.(type) {
		case nil:
		case string:
			fb = f
		default:
			b, _ := json.Marshal(f)
			fb = string(b)
		}
		if fb != "" {
			fb = "\n\nFeedback for session " + id + ":\n<FEEDBACK>\n" + fb + "\n</FEEDBACK>"
		}
		parts = append(parts, "<session_"+id+">\n"+messages.Buffer(messages.MergeRuns(t.Messages))+fb+"\n</session_"+id+">")
	}
	return strings.Join(parts, "\n\n")
}

// Optimize returns the improved prompt (or p.Prompt unchanged).
func (o *PromptOptimizer) Optimize(ctx context.Context, trajectories []Trajectory, p Prompt) (string, error) {
	return o.OptimizeSessions(ctx, FormatSessions(trajectories), p)
}

// OptimizeSessions is Optimize with already formatted session text (the
// string form of "trajectories" in LangMem).
func (o *PromptOptimizer) OptimizeSessions(ctx context.Context, sessions string, p Prompt) (string, error) {
	if o.Model == nil {
		return "", fmt.Errorf("langmem: PromptOptimizer.Model is required")
	}
	if strings.TrimSpace(sessions) == "" {
		return p.Prompt, nil // no trajectories, no change
	}
	cfg := o.Config
	if cfg.MaxReflectionSteps <= 0 {
		cfg.MaxReflectionSteps = 5
	}
	if cfg.MinReflectionSteps <= 0 {
		cfg.MinReflectionSteps = 1
	}
	switch o.Kind {
	case "", Gradient:
		return o.gradient(ctx, sessions, p, cfg)
	case Metaprompt:
		return o.metaprompt(ctx, sessions, p, cfg)
	case PromptMemory:
		return o.promptMemory(ctx, sessions, p)
	}
	return "", fmt.Errorf("langmem: unknown optimizer kind %q", o.Kind)
}

// ---- tools used while reflecting

var (
	thinkDef = tools.Definition{Name: "think", Description: "A reflection tool, used to reason over complexities and hypothesize fixes.",
		Parameters: obj(map[string]any{"thought": strProp("")}, "thought")}
	critiqueDef = tools.Definition{Name: "critique", Description: "A critique tool for diagnosing flaws in reasoning.",
		Parameters: obj(map[string]any{"criticism": strProp("")}, "criticism")}
	recommendDef = tools.Definition{Name: "recommend", Description: "Decides whether a prompt should be adjusted. If warrants_adjustment is true, incorporate the recommended changes; if not, respond 'No recommendations.'",
		Parameters: obj(map[string]any{
			"warrants_adjustment":  map[string]any{"type": "boolean"},
			"hypotheses":           strProp("Why the prompt underperforms"),
			"full_recommendations": strProp("Concrete changes to make"),
		}, "warrants_adjustment")}
)

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func strProp(desc string) map[string]any {
	p := map[string]any{"type": "string"}
	if desc != "" {
		p["description"] = desc
	}
	return p
}

func (o *PromptOptimizer) call(ctx context.Context, msgs []messages.Message, choice string, defs ...tools.Definition) (messages.Message, error) {
	return o.Model.Generate(ctx, msgs, models.WithToolDefinitions(defs...), models.WithToolChoice(choice))
}

// ackTools appends the model turn and an empty result for each tool call, so
// the next reflection round continues the same conversation.
func ackTools(msgs []messages.Message, ai messages.Message) []messages.Message {
	msgs = append(msgs, ai)
	for _, tc := range ai.ToolCalls {
		msgs = append(msgs, messages.Tool("", tc.ID))
	}
	return msgs
}

func findCall(ai messages.Message, name string) (messages.ToolCall, bool) {
	for _, tc := range ai.ToolCalls {
		if tc.Name == name {
			return tc, true
		}
	}
	return messages.ToolCall{}, false
}

// ---- gradient

func (o *PromptOptimizer) gradient(ctx context.Context, sessions string, p Prompt, cfg OptimizerConfig) (string, error) {
	tpl := cmp(cfg.GradientPrompt, defaultGradientPrompt)
	msgs := []messages.Message{messages.Human(fill(tpl, map[string]string{
		"prompt": p.Prompt, "update_instructions": p.UpdateInstructions, "trajectories": sessions, "feedback": ""}))}
	var rec messages.ToolCall
	found := false
	for ix := 0; ix < cfg.MaxReflectionSteps && !found; ix++ {
		var ai messages.Message
		var err error
		switch {
		case ix == cfg.MaxReflectionSteps-1:
			ai, err = o.call(ctx, msgs, "recommend", recommendDef)
		case ix < cfg.MinReflectionSteps:
			ai, err = o.call(ctx, msgs, "any", thinkDef, critiqueDef)
		default:
			ai, err = o.call(ctx, msgs, "any", thinkDef, critiqueDef, recommendDef)
		}
		if err != nil {
			return "", err
		}
		if rec, found = findCall(ai, "recommend"); !found {
			msgs = ackTools(msgs, ai)
		}
	}
	if !found {
		return "", fmt.Errorf("langmem: no final recommendation after %d reflection steps", cfg.MaxReflectionSteps)
	}
	if adj, _ := rec.Args["warrants_adjustment"].(bool); !adj {
		return p.Prompt, nil
	}
	hyp, _ := rec.Args["hypotheses"].(string)
	recs, _ := rec.Args["full_recommendations"].(string)
	update := fill(cmp(cfg.Metaprompt, defaultGradientUpdate), map[string]string{
		"current_prompt": p.Prompt, "hypotheses": hyp, "recommendations": recs, "update_instructions": p.UpdateInstructions})
	return o.rewrite(ctx, []messages.Message{messages.Human(update)}, p.Prompt)
}

// ---- metaprompt

func (o *PromptOptimizer) metaprompt(ctx context.Context, sessions string, p Prompt, cfg OptimizerConfig) (string, error) {
	tpl := cmp(cfg.Metaprompt, defaultMetaprompt)
	msgs := []messages.Message{messages.Human(fill(tpl, map[string]string{
		"prompt": p.Prompt, "update_instructions": p.UpdateInstructions, "trajectories": sessions}))}
	def := rewriteDef(p.Prompt)
	for ix := 0; ix < cfg.MaxReflectionSteps; ix++ {
		if ix == cfg.MaxReflectionSteps-1 {
			ai, err := o.call(ctx, msgs, def.Name, def)
			if err != nil {
				return "", err
			}
			return finishRewrite(ai, p.Prompt)
		}
		defs := []tools.Definition{thinkDef, critiqueDef, def}
		if ix < cfg.MinReflectionSteps-1 {
			defs = defs[:2]
		}
		ai, err := o.call(ctx, msgs, "any", defs...)
		if err != nil {
			return "", err
		}
		if _, ok := findCall(ai, def.Name); ok { // the model chose to finish early
			return finishRewrite(ai, p.Prompt)
		}
		msgs = ackTools(msgs, ai)
	}
	return p.Prompt, nil
}

// ---- prompt_memory

func (o *PromptOptimizer) promptMemory(ctx context.Context, sessions string, p Prompt) (string, error) {
	def := tools.Definition{Name: "GeneralResponse", Description: "Your decision and the new prompt.",
		Parameters: obj(map[string]any{
			"logic":         strProp("Reasoning about whether and how to change the prompt"),
			"update_prompt": map[string]any{"type": "boolean"},
			"new_prompt":    strProp("The full updated prompt, or empty if no update"),
		}, "logic", "update_prompt", "new_prompt")}
	in := fill(promptMemoryPrompt, map[string]string{"current_prompt": p.Prompt, "data": sessions, "instructions": p.UpdateInstructions})
	ai, err := o.call(ctx, []messages.Message{messages.Human(in)}, def.Name, def)
	if err != nil {
		return "", err
	}
	args := structuredArgs(ai, def.Name)
	if upd, _ := args["update_prompt"].(bool); !upd {
		return p.Prompt, nil
	}
	np, _ := args["new_prompt"].(string)
	if strings.TrimSpace(np) == "" {
		return p.Prompt, nil
	}
	return healVars(p.Prompt, np, false)
}

// ---- rewrite step shared by gradient and metaprompt

var varRe = regexp.MustCompile(`\{(.+?)\}`)

func rewriteDef(original string) tools.Definition {
	desc := " The prompt section being optimized contains no input f-string variables. Any brackets {{ foo }} you emit will be escaped and not used."
	if vars := promptVars(original); len(vars) > 0 {
		desc = " The prompt section being optimized contains the following f-string variables to be templated in: {" + strings.Join(vars, "}, {") + "}. You must retain all of these variables in your improved prompt. No other input variables are allowed."
	}
	return tools.Definition{Name: "OptimizedPromptOutput", Description: "Schema for the optimized prompt output.",
		Parameters: obj(map[string]any{
			"analysis":        strProp("First, analyze the current results and plan improvements to reconcile them."),
			"improved_prompt": strProp("Finally, generate the full updated prompt to address the identified issues." + desc),
		}, "analysis", "improved_prompt")}
}

func (o *PromptOptimizer) rewrite(ctx context.Context, msgs []messages.Message, original string) (string, error) {
	def := rewriteDef(original)
	ai, err := o.call(ctx, msgs, def.Name, def)
	if err != nil {
		return "", err
	}
	return finishRewrite(ai, original)
}

func finishRewrite(ai messages.Message, original string) (string, error) {
	imp, _ := structuredArgs(ai, "OptimizedPromptOutput")["improved_prompt"].(string)
	if strings.TrimSpace(imp) == "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(imp)), "no recommend") {
		return original, nil
	}
	return healVars(original, imp, true)
}

// structuredArgs reads a forced tool call's arguments, accepting a JSON text
// reply from models that ignore tool choice.
func structuredArgs(ai messages.Message, name string) map[string]any {
	if tc, ok := findCall(ai, name); ok {
		return tc.Args
	}
	if len(ai.ToolCalls) > 0 {
		return ai.ToolCalls[0].Args
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(models.ExtractJSON(ai.Text())), &m)
	return m
}

func promptVars(s string) []string {
	var out []string
	for _, m := range varRe.FindAllStringSubmatch(s, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

// healVars keeps the original prompt's {variables} intact in the rewrite:
// it fails if one went missing (when required), strips <TO_OPTIMIZE> markers
// and escapes any other stray braces as {{ }} (get_var_healer).
func healVars(original, rewritten string, required bool) (string, error) {
	vars := promptVars(original)
	if required {
		var missing []string
		for _, v := range vars {
			if !strings.Contains(rewritten, "{"+v+"}") {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("langmem: optimized prompt dropped required variable(s): %s", strings.Join(missing, ", "))
		}
	}
	masks := map[string]string{}
	for i, v := range vars {
		m := fmt.Sprintf("\x00VAR%d\x00", i)
		masks[m] = "{" + v + "}"
		rewritten = strings.ReplaceAll(rewritten, "{"+v+"}", m)
	}
	rewritten = regexp.MustCompile(`(?s)<TO_OPTIMIZE.*?>|</TO_OPTIMIZE>`).ReplaceAllString(rewritten, "")
	rewritten = regexp.MustCompile(`(^|[^{])\{([^{]|$)`).ReplaceAllString(rewritten, "$1{{$2")
	rewritten = regexp.MustCompile(`(^|[^}])\}([^}]|$)`).ReplaceAllString(rewritten, "$1}}$2")
	for m, v := range masks {
		rewritten = strings.ReplaceAll(rewritten, m, v)
	}
	return rewritten, nil
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- multi-prompt

// MultiPromptOptimizer improves several cooperating prompts together
// (create_multi_prompt_optimizer): a classifier assigns credit for the
// failures to the prompts that caused them (using each WhenToUpdate), then
// only those are optimized, concurrently.
type MultiPromptOptimizer struct {
	Model  models.ChatModel
	Kind   OptimizerKind
	Config OptimizerConfig
}

// Optimize returns the prompts in the same order, the implicated ones rewritten.
func (o *MultiPromptOptimizer) Optimize(ctx context.Context, trajectories []Trajectory, prompts []Prompt) ([]Prompt, error) {
	return o.OptimizeSessions(ctx, FormatSessions(trajectories), prompts)
}

// OptimizeSessions is Optimize over pre-formatted session text.
func (o *MultiPromptOptimizer) OptimizeSessions(ctx context.Context, sessions string, prompts []Prompt) ([]Prompt, error) {
	if o.Model == nil {
		return nil, fmt.Errorf("langmem: MultiPromptOptimizer.Model is required")
	}
	single := &PromptOptimizer{Model: o.Model, Kind: o.Kind, Config: o.Config}
	if len(prompts) == 1 && prompts[0].WhenToUpdate == "" {
		np, err := single.OptimizeSessions(ctx, sessions, prompts[0])
		if err != nil {
			return nil, err
		}
		p := prompts[0]
		p.Prompt = np
		return []Prompt{p}, nil
	}
	names := make([]string, len(prompts))
	var listing strings.Builder
	for i, p := range prompts {
		names[i] = p.Name
		fmt.Fprintf(&listing, "%s: %s\n", p.Name, p.Prompt)
		if p.WhenToUpdate != "" {
			fmt.Fprintf(&listing, "(update when: %s)\n", p.WhenToUpdate)
		}
	}
	def := tools.Definition{Name: "Classify",
		Description: "After analyzing the provided trajectories, determine which prompt modules (if any) contributed to degraded performance.",
		Parameters: obj(map[string]any{
			"reasoning": strProp("Reasoning for which prompts to update."),
			"which":     map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": names}, "description": "List of prompt names that should be updated."},
		}, "reasoning", "which")}
	ask := "Analyze the following trajectories and decide which prompts \nought to be updated to improve the performance on future trajectories:\n\n" + sessions +
		"\n\nBelow are the prompts being optimized:\n" + listing.String() + "\nReturn JSON with \"which\": [...], listing the names of prompts that need updates."
	ai, err := single.call(ctx, []messages.Message{messages.Human(ask)}, def.Name, def)
	if err != nil {
		return nil, err
	}
	var which []string
	if raw, ok := structuredArgs(ai, def.Name)["which"].([]any); ok {
		for _, r := range raw {
			s, _ := r.(string)
			if !slices.Contains(names, s) {
				return nil, fmt.Errorf("langmem: classifier returned unknown prompt %q (must be among %v)", s, names)
			}
			which = append(which, s)
		}
	}
	out := slices.Clone(prompts)
	errs := make([]error, len(out))
	var wg sync.WaitGroup
	for i, p := range out {
		if !slices.Contains(which, p.Name) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			np, err := single.OptimizeSessions(ctx, sessions, p)
			out[i].Prompt, errs[i] = np, err
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
