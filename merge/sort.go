package merge

import (
	"sort"

	"github.com/goccy/go-yaml"
)

// sortComponents produces deterministic output by (a) ordering component
// types in the OpenAPI-canonical order and (b) alphabetically sorting
// the keys within each type. Any unknown component types are preserved
// and appended alphabetically after the canonical ones.
func sortComponents(components *yaml.MapSlice) {
	if components == nil || len(*components) == 0 {
		return
	}

	order := make(map[string]int, len(componentTypes))
	for i, name := range componentTypes {
		order[name] = i
	}

	sort.SliceStable(*components, func(i, j int) bool {
		ki, _ := (*components)[i].Key.(string)
		kj, _ := (*components)[j].Key.(string)
		oi, iOK := order[ki]
		oj, jOK := order[kj]
		switch {
		case iOK && jOK:
			return oi < oj
		case iOK:
			return true
		case jOK:
			return false
		default:
			return ki < kj
		}
	})

	for i := range *components {
		inner, ok := (*components)[i].Value.(yaml.MapSlice)
		if !ok {
			continue
		}
		sort.SliceStable(inner, func(a, b int) bool {
			ka, _ := inner[a].Key.(string)
			kb, _ := inner[b].Key.(string)
			return ka < kb
		})
		(*components)[i].Value = inner
	}
}
