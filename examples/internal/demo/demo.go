// Package demo picks a real chat model when an API key is present and a
// scripted fake otherwise, so every example runs offline.
package demo

import (
	"fmt"
	"os"

	"github.com/fsvxavier/golang-chain-eco/chatmodels"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Model returns Anthropic/OpenAI (by env key) or a Fake replaying script.
func Model(script ...messages.Message) models.ChatModel {
	spec := ""
	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		spec = "anthropic:claude-sonnet-4-5"
	case os.Getenv("OPENAI_API_KEY") != "":
		spec = "openai:gpt-4o-mini"
	}
	if spec != "" {
		if m, err := chatmodels.Init(spec); err == nil {
			fmt.Println("(using", spec+")")
			return m
		}
	}
	fmt.Println("(no API key: using scripted fake model)")
	return models.NewFake(script...)
}

// Smart returns a real model when an API key is present, otherwise a fake
// whose replies are computed by fn from the conversation. It lets examples
// show multi-step behaviour offline without a long fixed script.
func Smart(fn func(msgs []messages.Message) messages.Message) models.ChatModel {
	if m := real(); m != nil {
		return m
	}
	fmt.Println("(no API key: using scripted fake model)")
	return &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		return fn(msgs), nil
	}}
}

func real() models.ChatModel {
	spec := ""
	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		spec = "anthropic:claude-sonnet-4-5"
	case os.Getenv("OPENAI_API_KEY") != "":
		spec = "openai:gpt-4o-mini"
	}
	if spec == "" {
		return nil
	}
	m, err := chatmodels.Init(spec)
	if err != nil {
		return nil
	}
	fmt.Println("(using", spec+")")
	return m
}

// Title prints a section header.
func Title(s string) { fmt.Printf("\n== %s ==\n", s) }

// Last returns the text of the last message in state["messages"].
func Last(state map[string]any) string {
	ms, _ := messages.Coerce(state["messages"])
	if m, ok := messages.Last(ms); ok {
		return m.Text()
	}
	return ""
}

// LastHuman returns the text of the last human message.
func LastHuman(msgs []messages.Message) string {
	if m, ok := messages.LastOfRole(msgs, messages.RoleHuman); ok {
		return m.Text()
	}
	return ""
}
