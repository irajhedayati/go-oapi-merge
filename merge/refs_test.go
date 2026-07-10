package merge

import (
	"reflect"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestResolveRefTable(t *testing.T) {
	tests := []struct {
		name     string
		rel      string
		current  string
		wantAbs  bool
		wantPath string
	}{
		{"empty relative returns empty", "", "api.yaml", false, ""},
		{"absolute path is returned unchanged", "/abs/path.yaml", "api.yaml", true, "/abs/path.yaml"},
		{"simple relative", "./schemas/user.yaml", "api.yaml", false, "schemas/user.yaml"},
		{"parent traversal", "../common.yaml", "sub/api.yaml", false, "common.yaml"},
		{"co-located file", "sibling.yaml", "specs/api.yaml", false, "specs/sibling.yaml"},
		{"deep nested descent", "./a/b/c.yaml", "top.yaml", false, "a/b/c.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRef(tt.rel, tt.current)
			if tt.wantAbs {
				if got != tt.wantPath {
					t.Errorf("got %q, want %q (absolute)", got, tt.wantPath)
				}
				return
			}
			if got != tt.wantPath {
				t.Errorf("got %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestFindRefs(t *testing.T) {
	tests := []struct {
		name        string
		input       yaml.MapSlice
		currentFile string
		want        yaml.MapSlice
		wantURLs    []string
	}{
		{
			name: "external ref is rewritten to local and file is queued",
			input: yaml.MapSlice{
				{Key: "$ref", Value: "./schemas.yaml#/components/schemas/User"},
			},
			currentFile: "specs/api.yaml",
			want: yaml.MapSlice{
				{Key: "$ref", Value: "#/components/schemas/User"},
			},
			wantURLs: []string{"specs/schemas.yaml"},
		},
		{
			name: "local ref is left untouched",
			input: yaml.MapSlice{
				{Key: "$ref", Value: "#/components/schemas/User"},
			},
			currentFile: "specs/api.yaml",
			want: yaml.MapSlice{
				{Key: "$ref", Value: "#/components/schemas/User"},
			},
			wantURLs: nil,
		},
		{
			name: "ref without # separator is left untouched",
			input: yaml.MapSlice{
				{Key: "$ref", Value: "just-a-string.yaml"},
			},
			currentFile: "specs/api.yaml",
			want: yaml.MapSlice{
				{Key: "$ref", Value: "just-a-string.yaml"},
			},
			wantURLs: nil,
		},
		{
			name: "non-string $ref value is left untouched",
			input: yaml.MapSlice{
				{Key: "$ref", Value: 42},
			},
			currentFile: "api.yaml",
			want: yaml.MapSlice{
				{Key: "$ref", Value: 42},
			},
			wantURLs: nil,
		},
		{
			name: "descends into nested MapSlice",
			input: yaml.MapSlice{
				{Key: "child", Value: yaml.MapSlice{
					{Key: "$ref", Value: "./other.yaml#/foo"},
				}},
			},
			currentFile: "api.yaml",
			want: yaml.MapSlice{
				{Key: "child", Value: yaml.MapSlice{
					{Key: "$ref", Value: "#/foo"},
				}},
			},
			wantURLs: []string{"other.yaml"},
		},
		{
			name: "descends into slice of MapSlice",
			input: yaml.MapSlice{
				{Key: "list", Value: []interface{}{
					yaml.MapSlice{{Key: "$ref", Value: "./a.yaml#/x"}},
					yaml.MapSlice{{Key: "$ref", Value: "./b.yaml#/y"}},
				}},
			},
			currentFile: "api.yaml",
			want: yaml.MapSlice{
				{Key: "list", Value: []interface{}{
					yaml.MapSlice{{Key: "$ref", Value: "#/x"}},
					yaml.MapSlice{{Key: "$ref", Value: "#/y"}},
				}},
			},
			wantURLs: []string{"a.yaml", "b.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls := make(map[string]bool)
			got := tt.input
			findRefs(&got, urls, tt.currentFile)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapslice mismatch\ngot:  %#v\nwant: %#v", got, tt.want)
			}

			gotURLs := make(map[string]bool, len(tt.wantURLs))
			for _, u := range tt.wantURLs {
				gotURLs[u] = true
			}
			if !reflect.DeepEqual(urls, gotURLs) {
				t.Errorf("urls = %v, want %v", urls, gotURLs)
			}
		})
	}
}

func TestFindRefs_NilURLsToParseIsSafe(t *testing.T) {
	input := yaml.MapSlice{
		{Key: "$ref", Value: "./other.yaml#/foo"},
	}
	// findRefs must not panic when urlsToParse is nil, and must still
	// rewrite the reference to its local form.
	findRefs(&input, nil, "api.yaml")

	if len(input) != 1 || input[0].Value != "#/foo" {
		t.Errorf("expected ref rewritten to #/foo, got %#v", input)
	}
}

func TestProcessValue(t *testing.T) {
	// processValue only recurses into container types; scalars must not
	// cause a panic or side effects.
	tests := []struct {
		name  string
		value interface{}
	}{
		{"nil value", nil},
		{"int scalar", 42},
		{"string scalar", "hello"},
		{"bool scalar", true},
		{"empty slice", []interface{}{}},
		{"unknown type", struct{ x int }{x: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls := make(map[string]bool)
			processValue(tt.value, urls, "api.yaml")
			if len(urls) != 0 {
				t.Errorf("expected no urls collected, got %v", urls)
			}
		})
	}
}

func TestProcessValue_RecursesIntoContainers(t *testing.T) {
	// Container types (MapSlice and []interface{}) must be recursed into.
	urls := make(map[string]bool)
	value := yaml.MapSlice{
		{Key: "$ref", Value: "./x.yaml#/a"},
	}
	processValue(value, urls, "api.yaml")

	if !urls["x.yaml"] {
		t.Errorf("expected x.yaml to be queued, got %v", urls)
	}
}
