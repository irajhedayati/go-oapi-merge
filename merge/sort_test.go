package merge

import (
	"reflect"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestSortComponents(t *testing.T) {
	// Build a component-type MapSlice compactly for readability.
	ct := func(name string, keys ...string) yaml.MapItem {
		inner := make(yaml.MapSlice, 0, len(keys))
		for _, k := range keys {
			inner = append(inner, yaml.MapItem{Key: k, Value: yaml.MapSlice{}})
		}
		return yaml.MapItem{Key: name, Value: inner}
	}

	tests := []struct {
		name     string
		input    yaml.MapSlice
		wantSort []string            // expected component-type order after sorting
		wantKeys map[string][]string // expected inner key order per type
	}{
		{
			name:     "nil input is a no-op",
			input:    nil,
			wantSort: nil,
		},
		{
			name:     "empty input is a no-op",
			input:    yaml.MapSlice{},
			wantSort: nil,
		},
		{
			name: "canonical order applied when input is scrambled",
			input: yaml.MapSlice{
				ct("callbacks", "B", "A"),
				ct("schemas", "Zebra", "Apple"),
				ct("responses", "NotFound", "OK"),
			},
			wantSort: []string{"schemas", "responses", "callbacks"},
			wantKeys: map[string][]string{
				"schemas":   {"Apple", "Zebra"},
				"responses": {"NotFound", "OK"},
				"callbacks": {"A", "B"},
			},
		},
		{
			name: "unknown types appended alphabetically after canonical",
			input: yaml.MapSlice{
				ct("xExtension", "b", "a"),
				ct("aExtension", "y", "x"),
				ct("schemas", "Z", "A"),
			},
			wantSort: []string{"schemas", "aExtension", "xExtension"},
			wantKeys: map[string][]string{
				"schemas":    {"A", "Z"},
				"aExtension": {"x", "y"},
				"xExtension": {"a", "b"},
			},
		},
		{
			name: "already-sorted input remains unchanged",
			input: yaml.MapSlice{
				ct("schemas", "A", "B", "C"),
				ct("responses", "OK"),
			},
			wantSort: []string{"schemas", "responses"},
			wantKeys: map[string][]string{
				"schemas":   {"A", "B", "C"},
				"responses": {"OK"},
			},
		},
		{
			name: "non-MapSlice inner values are left alone",
			input: yaml.MapSlice{
				{Key: "schemas", Value: "unexpected scalar"},
				ct("responses", "OK"),
			},
			wantSort: []string{"schemas", "responses"},
			// wantKeys omits schemas since it's a scalar.
			wantKeys: map[string][]string{
				"responses": {"OK"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input
			sortComponents(&got)

			var gotOrder []string
			for _, item := range got {
				gotOrder = append(gotOrder, item.Key.(string))
			}
			if !reflect.DeepEqual(gotOrder, tt.wantSort) {
				t.Errorf("component-type order = %v, want %v", gotOrder, tt.wantSort)
			}

			for compType, wantKeys := range tt.wantKeys {
				inner, ok := getMapSliceValue(got, compType).(yaml.MapSlice)
				if !ok {
					t.Errorf("expected %q to be a MapSlice", compType)
					continue
				}
				gotKeys := make([]string, 0, len(inner))
				for _, it := range inner {
					gotKeys = append(gotKeys, it.Key.(string))
				}
				if !reflect.DeepEqual(gotKeys, wantKeys) {
					t.Errorf("%q keys = %v, want %v", compType, gotKeys, wantKeys)
				}
			}
		})
	}
}

func TestSortComponents_IsStable(t *testing.T) {
	// If two canonical types tie in order (impossible here since each is
	// unique), stability doesn't matter. But for unknown types with
	// identical names — impossible — stability is trivially satisfied.
	// This test guards against future changes accidentally introducing
	// a non-stable sort by asserting a deterministic result across runs.
	input := yaml.MapSlice{
		{Key: "schemas", Value: yaml.MapSlice{
			{Key: "B", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
			{Key: "A", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
		}},
	}

	sortComponents(&input)
	first := keysDeep(input)

	// Re-sort an already-sorted copy and ensure the result is identical.
	again := input
	sortComponents(&again)
	if !reflect.DeepEqual(first, keysDeep(again)) {
		t.Errorf("second sort changed the ordering: %v vs %v", first, keysDeep(again))
	}
}

// keysDeep returns "type:key" pairs in order for equality comparison.
func keysDeep(m yaml.MapSlice) []string {
	var out []string
	for _, ct := range m {
		typeName := ct.Key.(string)
		inner, ok := ct.Value.(yaml.MapSlice)
		if !ok {
			out = append(out, typeName+":<scalar>")
			continue
		}
		for _, item := range inner {
			out = append(out, typeName+":"+item.Key.(string))
		}
	}
	return out
}
