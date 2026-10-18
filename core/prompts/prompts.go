// Package prompts implements prompt templates (langchain_core.prompts):
// f-string PromptTemplate, ChatPromptTemplate with MessagesPlaceholder, and
// few-shot templates. Every template is a runnables.Runnable.
package prompts

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

// Values are the template input variables.
type Values = map[string]any

// Format renders an f-string template: {name} is substituted and {{ / }}
// produce literal braces. Missing variables are an error.
func Format(tmpl string, v Values) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		switch {
		case c == '{' && i+1 < len(tmpl) && tmpl[i+1] == '{':
			sb.WriteByte('{')
			i++
		case c == '}' && i+1 < len(tmpl) && tmpl[i+1] == '}':
			sb.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(tmpl[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("prompts: unclosed '{' at %d", i)
			}
			name := strings.TrimSpace(tmpl[i+1 : i+end])
			val, ok := v[name]
			if !ok {
				return "", fmt.Errorf("prompts: missing variable %q", name)
			}
			sb.WriteString(fmt.Sprint(val))
			i += end
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String(), nil
}

// Variables lists the variables referenced by an f-string template.
func Variables(tmpl string) []string {
	seen := map[string]bool{}
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] == '{' {
			if i+1 < len(tmpl) && tmpl[i+1] == '{' {
				i++
				continue
			}
			if end := strings.IndexByte(tmpl[i:], '}'); end > 0 {
				seen[strings.TrimSpace(tmpl[i+1:i+end])] = true
				i += end
			}
		} else if tmpl[i] == '}' && i+1 < len(tmpl) && tmpl[i+1] == '}' {
			i++
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PromptTemplate is a string template.
type PromptTemplate struct {
	Template string
	Partials Values
}

// New creates a PromptTemplate (PromptTemplate.from_template).
func New(tmpl string) *PromptTemplate { return &PromptTemplate{Template: tmpl} }

// Partial returns a copy with some variables pre-filled.
func (p *PromptTemplate) Partial(v Values) *PromptTemplate {
	c := *p
	c.Partials = merge(p.Partials, v)
	return &c
}

// InputVariables lists the variables still required.
func (p *PromptTemplate) InputVariables() []string { return without(Variables(p.Template), p.Partials) }

// Format renders the template.
func (p *PromptTemplate) Format(v Values) (string, error) {
	return Format(p.Template, merge(p.Partials, v))
}

// Invoke implements runnables.Runnable.
func (p *PromptTemplate) Invoke(_ context.Context, v Values) (string, error) { return p.Format(v) }

// MessageTemplate produces zero or more messages from values.
type MessageTemplate interface {
	FormatMessages(v Values) ([]messages.Message, error)
}

type roleTemplate struct {
	role messages.Role
	tmpl string
}

func (r roleTemplate) FormatMessages(v Values) ([]messages.Message, error) {
	s, err := Format(r.tmpl, v)
	if err != nil {
		return nil, err
	}
	return []messages.Message{{Role: r.role, Content: s}}, nil
}

// SystemTemplate, HumanTemplate and AITemplate build role templates.
func SystemTemplate(t string) MessageTemplate { return roleTemplate{messages.RoleSystem, t} }

// HumanTemplate builds a human message template.
func HumanTemplate(t string) MessageTemplate { return roleTemplate{messages.RoleHuman, t} }

// AITemplate builds an AI message template.
func AITemplate(t string) MessageTemplate { return roleTemplate{messages.RoleAI, t} }

// Placeholder inserts the message list stored under Name (MessagesPlaceholder).
type Placeholder struct {
	Name     string
	Optional bool
}

// FormatMessages implements MessageTemplate.
func (p Placeholder) FormatMessages(v Values) ([]messages.Message, error) {
	raw, ok := v[p.Name]
	if !ok {
		if p.Optional {
			return nil, nil
		}
		return nil, fmt.Errorf("prompts: missing messages placeholder %q", p.Name)
	}
	return messages.Coerce(raw)
}

// Static is a fixed message (no templating).
type Static messages.Message

// FormatMessages implements MessageTemplate.
func (s Static) FormatMessages(Values) ([]messages.Message, error) {
	return []messages.Message{messages.Message(s)}, nil
}

// ChatPromptTemplate renders a list of message templates.
type ChatPromptTemplate struct {
	Messages []MessageTemplate
	Partials Values
}

// Chat builds a ChatPromptTemplate. Items may be MessageTemplate values,
// [2]string{role, template} pairs (role: system/human/user/ai/assistant/
// placeholder) or messages.Message (static).
func Chat(items ...any) *ChatPromptTemplate {
	c := &ChatPromptTemplate{}
	for _, it := range items {
		switch x := it.(type) {
		case MessageTemplate:
			c.Messages = append(c.Messages, x)
		case messages.Message:
			c.Messages = append(c.Messages, Static(x))
		case [2]string:
			switch x[0] {
			case "system":
				c.Messages = append(c.Messages, SystemTemplate(x[1]))
			case "human", "user":
				c.Messages = append(c.Messages, HumanTemplate(x[1]))
			case "ai", "assistant":
				c.Messages = append(c.Messages, AITemplate(x[1]))
			case "placeholder":
				name := strings.Trim(x[1], "{} ")
				c.Messages = append(c.Messages, Placeholder{Name: name, Optional: true})
			default:
				panic("prompts: unknown role " + x[0])
			}
		default:
			panic(fmt.Sprintf("prompts: unsupported chat prompt item %T", it))
		}
	}
	return c
}

// Partial returns a copy with pre-filled variables.
func (c *ChatPromptTemplate) Partial(v Values) *ChatPromptTemplate {
	cp := *c
	cp.Partials = merge(c.Partials, v)
	return &cp
}

// Append adds message templates and returns c.
func (c *ChatPromptTemplate) Append(ms ...MessageTemplate) *ChatPromptTemplate {
	c.Messages = append(c.Messages, ms...)
	return c
}

// FormatMessages renders all messages.
func (c *ChatPromptTemplate) FormatMessages(v Values) ([]messages.Message, error) {
	v = merge(c.Partials, v)
	var out []messages.Message
	for _, m := range c.Messages {
		ms, err := m.FormatMessages(v)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

// Invoke implements runnables.Runnable[Values, []messages.Message].
func (c *ChatPromptTemplate) Invoke(_ context.Context, v Values) ([]messages.Message, error) {
	return c.FormatMessages(v)
}

// Example is one few-shot example.
type Example = map[string]any

// ExampleSelector chooses examples for an input (BaseExampleSelector).
type ExampleSelector interface {
	Select(input Values) []Example
}

// LengthSelector picks as many examples as fit within MaxLength words
// (LengthBasedExampleSelector).
type LengthSelector struct {
	Examples  []Example
	Template  *PromptTemplate
	MaxLength int
}

// Select implements ExampleSelector.
func (l LengthSelector) Select(input Values) []Example {
	budget := l.MaxLength
	for _, v := range input {
		budget -= len(strings.Fields(fmt.Sprint(v)))
	}
	var out []Example
	for _, ex := range l.Examples {
		s, _ := l.Template.Format(ex)
		n := len(strings.Fields(s))
		if budget-n < 0 {
			break
		}
		budget -= n
		out = append(out, ex)
	}
	return out
}

// FewShot is FewShotPromptTemplate: prefix, rendered examples, suffix.
type FewShot struct {
	Examples        []Example
	Selector        ExampleSelector
	ExampleTemplate *PromptTemplate
	Prefix, Suffix  string
	Separator       string // default "\n\n"
}

// Format renders the few-shot prompt.
func (f FewShot) Format(v Values) (string, error) {
	exs := f.Examples
	if f.Selector != nil {
		exs = f.Selector.Select(v)
	}
	sep := f.Separator
	if sep == "" {
		sep = "\n\n"
	}
	var parts []string
	if f.Prefix != "" {
		p, err := Format(f.Prefix, v)
		if err != nil {
			return "", err
		}
		parts = append(parts, p)
	}
	for _, ex := range exs {
		s, err := f.ExampleTemplate.Format(ex)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	if f.Suffix != "" {
		s, err := Format(f.Suffix, v)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, sep), nil
}

// Invoke implements runnables.Runnable.
func (f FewShot) Invoke(_ context.Context, v Values) (string, error) { return f.Format(v) }

// FewShotChat renders each example through a chat template
// (FewShotChatMessagePromptTemplate); use it inside Chat(...).
type FewShotChat struct {
	Examples        []Example
	Selector        ExampleSelector
	ExampleTemplate *ChatPromptTemplate
}

// FormatMessages implements MessageTemplate.
func (f FewShotChat) FormatMessages(v Values) ([]messages.Message, error) {
	exs := f.Examples
	if f.Selector != nil {
		exs = f.Selector.Select(v)
	}
	var out []messages.Message
	for _, ex := range exs {
		ms, err := f.ExampleTemplate.FormatMessages(ex)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

func merge(a, b Values) Values {
	if len(a) == 0 {
		return b
	}
	out := maps.Clone(a)
	maps.Copy(out, b)
	return out
}

func without(vars []string, have Values) []string {
	var out []string
	for _, v := range vars {
		if _, ok := have[v]; !ok {
			out = append(out, v)
		}
	}
	return out
}
