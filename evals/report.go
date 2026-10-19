package evals

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
	"time"
)

// WriteText prints one row per example (a column per score key) followed by
// the mean per key.
func WriteText(w io.Writer, r Report) error {
	keys := make([]string, 0, len(r.Summary))
	for k := range r.Summary {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "EXAMPLE\tLATENCY")
	for _, k := range keys {
		fmt.Fprintf(tw, "\t%s", k)
	}
	fmt.Fprintln(tw, "\tERROR")
	for _, ex := range r.Results {
		fmt.Fprintf(tw, "%s\t%s", ex.ExampleID, ex.Latency.Round(time.Millisecond))
		scores := map[string]float64{}
		for _, s := range ex.Results {
			scores[s.Key] = s.Score
		}
		for _, k := range keys {
			if v, ok := scores[k]; ok {
				fmt.Fprintf(tw, "\t%.2f", v)
			} else {
				fmt.Fprint(tw, "\t-")
			}
		}
		fmt.Fprintf(tw, "\t%s\n", firstNonEmpty(ex.Error, joinFirst(ex.EvalErrors)))
	}
	fmt.Fprint(tw, "MEAN\t")
	for _, k := range keys {
		fmt.Fprintf(tw, "\t%.2f", r.Summary[k])
	}
	fmt.Fprintln(tw, "\t")
	return tw.Flush()
}

// WriteJSON prints the whole report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func joinFirst(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
