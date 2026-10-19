// prompt_optimizer: procedural memory. A system prompt learns from
// conversations plus feedback; a multi-prompt optimizer decides which of
// several cooperating prompts caused the failure (create_prompt_optimizer /
// create_multi_prompt_optimizer).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

func main() {
	ctx := context.Background()
	trajectories := []langmem.Trajectory{
		{Messages: []messages.Message{
			messages.Human("Explain inheritance in Python"),
			messages.AI("Inheritance is a mechanism where a class derives from another..."),
			messages.Human("Show me a practical example instead")},
			Feedback: map[string]any{"score": 0, "comment": "too theoretical, wanted code"}},
	}

	demo.Title("single prompt, kind=metaprompt")
	// Offline stand-in: the optimizer's reflect/rewrite protocol, scripted.
	rewriter := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		for _, d := range o.Tools {
			if d.Name == "OptimizedPromptOutput" {
				return messages.AI("", messages.ToolCall{ID: "1", Name: d.Name, Args: map[string]any{
					"analysis":        "The user asked for code and got theory.",
					"improved_prompt": "You are a helpful assistant.\n\nWhen explaining programming concepts, lead with a short practical example in code, then add theory only if asked.",
				}}), nil
			}
		}
		return messages.AI("", messages.ToolCall{ID: "0", Name: "think", Args: map[string]any{"thought": "Failure mode: style mismatch."}}), nil
	}}
	opt := langmem.NewPromptOptimizer(rewriter, langmem.Metaprompt, langmem.OptimizerConfig{MaxReflectionSteps: 2})
	better, err := opt.Optimize(ctx, trajectories, langmem.Prompt{Name: "main", Prompt: "You are a helpful assistant."})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(better)

	demo.Title("multi prompt: credit assignment")
	multi := &models.Fake{Func: func(msgs []messages.Message, o models.Options) (messages.Message, error) {
		switch o.Tools[0].Name {
		case "Classify":
			return messages.AI("", messages.ToolCall{ID: "1", Name: "Classify", Args: map[string]any{
				"reasoning": "The failure is in the explanation style, not research.", "which": []any{"explain"}}}), nil
		default:
			return messages.AI("", messages.ToolCall{ID: "2", Name: "GeneralResponse", Args: map[string]any{
				"logic": "needs code", "update_prompt": true, "new_prompt": "Explain the concept with a runnable code example first."}}), nil
		}
	}}
	mo := &langmem.MultiPromptOptimizer{Model: multi, Kind: langmem.PromptMemory}
	prompts := []langmem.Prompt{
		{Name: "research", Prompt: "Research the topic thoroughly.", WhenToUpdate: "when facts are wrong or missing"},
		{Name: "explain", Prompt: "Explain the concept.", WhenToUpdate: "when the explanation style misses the user's needs"},
	}
	out, err := mo.Optimize(ctx, trajectories, prompts)
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range out {
		changed := "unchanged"
		if p.Prompt != prompts[indexOf(prompts, p.Name)].Prompt {
			changed = "UPDATED"
		}
		fmt.Printf("%-9s %-9s %s\n", p.Name, changed, strings.ReplaceAll(p.Prompt, "\n", " "))
	}
	_ = demo.Title
}

func indexOf(ps []langmem.Prompt, name string) int {
	for i, p := range ps {
		if p.Name == name {
			return i
		}
	}
	return 0
}
