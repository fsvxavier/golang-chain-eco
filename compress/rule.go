package compress

import (
	"context"
	"fmt"
	"strings"
)

// Defaults used when a RuleDecider field is zero.
const (
	defaultHistoryTokens = 4000
	defaultShellMinBytes = 2048
	ruleCodeShare        = 0.1  // ponytail when code is at least this share of chat bytes
	ruleJSONBytes        = 2048 // toon_tool_results when JSON tool bytes reach this
)

var defaultRTKCommands = []string{"git", "go", "ls", "grep", "find", "docker", "kubectl", "npm", "cargo", "pytest"}

// RuleDecider answers with fixed thresholds and needs no model. It is the
// fallback behind Guarded. The zero value uses the default thresholds.
type RuleDecider struct {
	HistoryTokens int      // compress history above this many estimated tokens (default 4000)
	ShellMinBytes int      // route a command when its last output was above this (default 2048)
	RTKCommands   []string // commands eligible for rtk (default git go ls grep find docker kubectl npm cargo pytest)
}

// Decide answers each question by id. compress_reply follows the history
// decision. Unknown ids are an error.
func (r RuleDecider) Decide(_ context.Context, st State, qs []Question) ([]Answer, error) {
	historyTokens := float64(r.HistoryTokens)
	if r.HistoryTokens <= 0 {
		historyTokens = defaultHistoryTokens
	}
	shellMin := float64(r.ShellMinBytes)
	if r.ShellMinBytes <= 0 {
		shellMin = defaultShellMinBytes
	}
	rtk := r.RTKCommands
	if len(rtk) == 0 {
		rtk = defaultRTKCommands
	}

	tokens, _ := ruleNum(st, "tokens")
	compress := tokens > historyTokens
	codeShare, _ := ruleNum(st, "code_share")
	jsonBytes, _ := ruleNum(st, "json_tool_bytes")

	out := make([]Answer, 0, len(qs))
	for _, q := range qs {
		switch q.ID {
		case QCompressHistory, QCompressReply:
			out = append(out, ruleNoul(q, compress))
		case QLevel:
			out = append(out, Answer{ID: q.ID, Kind: KindChoice, Pick: "full", Dist: map[string]float64{"full": 1}})
		case QPonytail:
			out = append(out, ruleNoul(q, codeShare >= ruleCodeShare))
		case QTOON:
			out = append(out, ruleNoul(q, jsonBytes >= ruleJSONBytes))
		case QRouteRTK:
			command, _ := st["command"].(string)
			route := false
			if fields := strings.Fields(command); len(fields) > 0 && ruleContains(rtk, fields[0]) {
				prev, ok := ruleNum(st, "prev_output_bytes")
				route = !ok || prev < 0 || prev > shellMin
			}
			out = append(out, ruleNoul(q, route))
		default:
			return nil, fmt.Errorf("compress: rule decider: unknown question %q", q.ID)
		}
	}
	return out, nil
}

func ruleNoul(q Question, v bool) Answer {
	p := 0.0
	if v {
		p = 1
	}
	return Answer{ID: q.ID, Kind: q.Kind, True: v, P: p}
}

// ruleNum reads a number that may come from Go code (int) or from decoded
// JSON (float64).
func ruleNum(st State, key string) (float64, bool) {
	switch v := st[key].(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

func ruleContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
