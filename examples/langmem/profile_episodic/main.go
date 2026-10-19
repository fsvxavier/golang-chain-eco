// profile_episodic: the three semantic/episodic memory shapes of LangMem:
// a collection (insert/update/delete), a single profile document, and
// episodes (how a situation was handled).
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// UserProfile is a strict schema: the model can only fill these fields.
type UserProfile struct {
	Name           string   `json:"name"`
	PreferredName  string   `json:"preferred_name"`
	ResponseStyle  string   `json:"response_style_preference"`
	SpecialSkills  []string `json:"special_skills"`
	OtherPrefsList []string `json:"other_preferences"`
}

func dump(ctx context.Context, st store.Store, ns []string) {
	items, _ := st.Search(ctx, ns, nil, 0)
	for _, it := range items {
		fmt.Printf("  %-10s %-8s %v\n", it.Key[:min(10, len(it.Key))], it.Value["kind"], it.Value["content"])
	}
}

func main() {
	ctx := langmem.WithConfigurable(context.Background(), "langgraph_user_id", "lex")
	ns := []string{"memories", "lex"}

	// 1. Collection: facts accumulate, change and disappear.
	demo.Title("collection: insert, then update + delete")
	st1 := store.NewMemory()
	turn := 0
	collection := demo.Smart(func(msgs []messages.Message) messages.Message {
		turn++
		if turn == 1 {
			return messages.AI("",
				messages.ToolCall{ID: "1", Name: "Memory", Args: map[string]any{"content": "Likes dark mode"}},
				messages.ToolCall{ID: "2", Name: "Memory", Args: map[string]any{"content": "Lives in Rome"}})
		}
		// the extractor sees ids in the prompt: update dark mode, delete Rome
		prompt := msgs[1].Text()
		idOf := func(text string) string {
			i := strings.Index(prompt, text)
			j := strings.LastIndex(prompt[:i], `id="`) + 4
			return prompt[j : j+strings.Index(prompt[j:], `"`)]
		}
		return messages.AI("",
			messages.ToolCall{ID: "3", Name: "Memory", Args: map[string]any{"content": "Dislikes dark mode (eyes hurt)", "json_doc_id": idOf("Likes dark mode")}},
			messages.ToolCall{ID: "4", Name: "RemoveDoc", Args: map[string]any{"json_doc_id": idOf("Lives in Rome")}})
	})
	sm := langmem.NewStoreManager(collection, nil, langmem.WithStore(st1))
	if _, err := sm.Process(ctx, []messages.Message{messages.Human("I like dark mode and I live in Rome")}); err != nil {
		log.Fatal(err)
	}
	dump(ctx, st1, ns)
	fmt.Println(" -- after: 'dark mode hurts my eyes, and I moved away from Rome'")
	if _, err := sm.Process(ctx, []messages.Message{messages.Human("Dark mode hurts my eyes, and I moved away from Rome")}); err != nil {
		log.Fatal(err)
	}
	dump(ctx, st1, ns)

	// 2. Profile: one document, refined in place. Seeded, inserts disabled.
	demo.Title("profile: a single strict document")
	st2 := store.NewMemory()
	profileModel := demo.Smart(func([]messages.Message) messages.Message {
		return messages.AI("", messages.ToolCall{ID: "1", Name: "UserProfile", Args: map[string]any{
			"json_doc_id": langmem.DefaultKey, "name": "Alex", "preferred_name": "Lex",
			"response_style_preference": "casual and witty",
			"special_skills":            []any{"Python", "speedcubing"}, "other_preferences": []any{"no corporate tone"}}})
	})
	pm := langmem.NewStoreManager(profileModel, nil,
		langmem.WithStore(st2),
		langmem.WithSchemas(langmem.SchemaFor[UserProfile]("UserProfile", "Save the user's preferences.")),
		langmem.WithInserts(false), langmem.WithDeletes(false),
		langmem.WithDefault(langmem.Seed{Content: UserProfile{}}))
	if _, err := pm.Process(ctx, []messages.Message{messages.Human("I'm Alex, call me Lex. Keep it casual and witty. I do speedcubing.")}); err != nil {
		log.Fatal(err)
	}
	dump(ctx, st2, ns)
	it, _ := pm.Get(ctx, langmem.DefaultKey)
	p, _ := langmem.Decode[UserProfile](it.Value["content"])
	fmt.Printf("  typed: %s goes by %q, style=%q\n", p.Name, p.PreferredName, p.ResponseStyle)

	// 3. Episodes: remember how something was handled, to reuse the approach.
	demo.Title("episodic: how a situation was handled")
	st3 := store.NewMemory()
	episodeModel := demo.Smart(func([]messages.Message) messages.Message {
		return messages.AI("", messages.ToolCall{ID: "1", Name: "Episode", Args: map[string]any{
			"observation": "User knows family trees, asked what a binary tree is",
			"thoughts":    "Anchor the new concept in a domain the user already knows",
			"action":      "Explained binary trees as family trees with at most two children",
			"result":      "User immediately connected it to binary search trees"}})
	})
	em := langmem.NewStoreManager(episodeModel, nil, langmem.WithStore(st3),
		langmem.WithSchemas(langmem.EpisodeSchema),
		langmem.WithInstructions("Extract examples of successful interactions: context, thought process, and why it worked."))
	if _, err := em.Process(ctx, []messages.Message{
		messages.Human("What's a binary tree? I work with family trees."),
		messages.AI("Like a family tree where each parent has at most 2 children."),
		messages.Human("Oh! So a binary search tree is like organising by age?")}); err != nil {
		log.Fatal(err)
	}
	dump(ctx, st3, ns)
}
