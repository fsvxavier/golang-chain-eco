package babysit

import "encoding/json"

func encode(w *Watch) (map[string]any, error) {
	b, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func decode(m map[string]any) (*Watch, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var w Watch
	return &w, json.Unmarshal(b, &w)
}
