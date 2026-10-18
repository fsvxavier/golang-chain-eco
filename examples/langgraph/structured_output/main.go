// structured_output: an agent that finishes with a typed, validated answer
// (port of react-agent-structured-output.ipynb).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/examples/internal/demo"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

type WeatherReport struct {
	City        string  `json:"city"`
	TempC       float64 `json:"temp_c" description:"Temperature in Celsius"`
	Description string  `json:"description"`
}

type CityArgs struct {
	City string `json:"city"`
}

func main() {
	weather := tools.New("get_weather", "Weather for a city.", func(_ context.Context, a CityArgs) (any, error) {
		return "Sunny, 21C", nil
	})
	model := demo.Smart(func(msgs []messages.Message) messages.Message {
		if last := msgs[len(msgs)-1]; last.Role == messages.RoleTool {
			return messages.AI(`{"city":"Oslo","temp_c":21,"description":"Sunny"}`)
		}
		return messages.AI("", messages.ToolCall{Name: "get_weather", Args: map[string]any{"city": "Oslo"}})
	})
	agent, err := agents.Create(agents.Options{
		Model: model, Tools: []tools.Tool{weather},
		ResponseFormat: &models.ResponseFormat{Name: "WeatherReport", Schema: jsonschema.For[WeatherReport]().Map()},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Weather in Oslo? Reply as JSON."})
	if err != nil {
		log.Fatal(err)
	}
	var r WeatherReport
	b, _ := json.Marshal(out["structured_response"])
	_ = json.Unmarshal(b, &r)
	fmt.Printf("typed result: %+v\n", r)
}
