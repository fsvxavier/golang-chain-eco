// compress_toon: encodes a JSON tool result as TOON and shows the size before
// and after, plus the TOONShorter fallback for a tiny value.
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/compress"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

type user struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Active bool   `json:"active"`
	Team   string `json:"team"`
}

func main() {
	rows := users(20)
	raw, err := json.Marshal(map[string]any{"users": rows})
	if err != nil {
		log.Fatal(err)
	}
	text := string(raw)

	toon, err := compress.TOONFromJSON(text, compress.TOONOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if len(toon) >= len(text) {
		log.Fatalf("TOON is not shorter: %d >= %d bytes", len(toon), len(text))
	}

	before := messages.ApproxTokens([]messages.Message{messages.Tool(text, "call-users")})
	after := messages.ApproxTokens([]messages.Message{messages.Tool(toon, "call-users")})

	fmt.Println("== JSON tool result ==")
	fmt.Println(text)
	fmt.Println()
	fmt.Println("== TOON ==")
	fmt.Println(toon)
	fmt.Println()
	fmt.Printf("bytes: %d -> %d (saved %d)\n", len(text), len(toon), len(text)-len(toon))
	fmt.Printf("estimated tokens: %d -> %d\n", before, after)
	fmt.Println()

	// A one-element array is shorter as JSON, so TOONShorter keeps the text.
	tiny := "[1]"
	out, ok := compress.TOONShorter(tiny)
	fmt.Println("== TOONShorter fallback ==")
	fmt.Printf("input: %s\n", tiny)
	fmt.Printf("rewritten: %v, output: %s\n", ok, out)
	if ok {
		log.Fatal("TOONShorter rewrote a value that is not shorter in TOON")
	}
}

// users returns n deterministic rows so the output is stable across runs.
func users(n int) []user {
	teams := []string{"core", "platform", "data"}
	rows := make([]user, 0, n)
	for i := 1; i <= n; i++ {
		rows = append(rows, user{
			ID:     i,
			Name:   fmt.Sprintf("User %02d", i),
			Email:  fmt.Sprintf("user%02d@example.com", i),
			Active: i%3 != 0,
			Team:   teams[i%len(teams)],
		})
	}
	return rows
}
