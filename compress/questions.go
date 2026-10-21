package compress

// Question ids. Their values are part of the decider contract.
const (
	QCompressHistory = "compress_history"
	QLevel           = "level"
	QCompressReply   = "compress_reply"
	QRouteRTK        = "route_rtk"
)

// HistoryQuestions returns the questions about compressing old turns, in the
// order compress_history, level, compress_reply.
func HistoryQuestions() []Question {
	return []Question{
		{ID: QCompressHistory, Kind: KindNoul, Text: "Compress the older turns of this conversation?"},
		{ID: QLevel, Kind: KindChoice, Text: "Caveman level for the older turns.", Options: []string{"lite", "full", "ultra"}},
		{ID: QCompressReply, Kind: KindNoul, Text: "Ask the model to reply tersely?"},
	}
}

// ShellQuestion asks whether a shell command should be routed through rtk.
func ShellQuestion() Question {
	return Question{ID: QRouteRTK, Kind: KindNoul, Text: "Route this shell command through rtk?"}
}

// ShellState is the State for ShellQuestion. prevOutputBytes is the last
// output size seen for this command, or -1 when unknown.
func ShellState(command string, prevOutputBytes int) State {
	return State{"command": command, "prev_output_bytes": prevOutputBytes}
}
