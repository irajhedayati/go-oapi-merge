package merge

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestCollectPendingURLs(t *testing.T) {
	tests := []struct {
		name      string
		urls      map[string]bool
		processed map[string]bool
		want      []string
	}{
		{
			name:      "all fresh",
			urls:      map[string]bool{"a": true, "b": true, "c": true},
			processed: map[string]bool{},
			want:      []string{"a", "b", "c"},
		},
		{
			name:      "some already processed",
			urls:      map[string]bool{"a": true, "b": true, "c": true},
			processed: map[string]bool{"b": true},
			want:      []string{"a", "c"},
		},
		{
			name:      "all processed",
			urls:      map[string]bool{"a": true},
			processed: map[string]bool{"a": true},
			want:      nil,
		},
		{
			name:      "empty urls",
			urls:      map[string]bool{},
			processed: map[string]bool{},
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectPendingURLs(tt.urls, tt.processed)
			// Order is not guaranteed by collectPendingURLs (map iteration);
			// sort both sides before comparing.
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestMergeComponentsUnit(t *testing.T) {
	tests := []struct {
		name        string
		nested      yaml.MapSlice
		existing    yaml.MapSlice
		currentFile string
		wantTypes   map[string][]string // component type -> ordered list of keys expected
		wantQueued  []string
	}{
		{
			name: "new schemas appended",
			nested: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "User", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
					{Key: "Order", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
				}},
			},
			existing:    yaml.MapSlice{},
			currentFile: "schemas.yaml",
			wantTypes: map[string][]string{
				"schemas": {"User", "Order"},
			},
		},
		{
			name: "duplicate keys are dropped in favor of existing",
			nested: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "User", Value: yaml.MapSlice{{Key: "type", Value: "STOMPED"}}},
					{Key: "Order", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
				}},
			},
			existing: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "User", Value: yaml.MapSlice{{Key: "type", Value: "ORIGINAL"}}},
				}},
			},
			currentFile: "schemas.yaml",
			wantTypes: map[string][]string{
				"schemas": {"User", "Order"},
			},
		},
		{
			name: "external $ref inside components is queued for merging",
			nested: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "User", Value: yaml.MapSlice{
						{Key: "$ref", Value: "./other.yaml#/components/schemas/UserBase"},
					}},
				}},
			},
			existing:    yaml.MapSlice{},
			currentFile: "specs/schemas.yaml",
			wantTypes: map[string][]string{
				"schemas": {"User"},
			},
			wantQueued: []string{"specs/other.yaml"},
		},
		{
			name: "non-component keys inside nestedComponents are ignored",
			nested: yaml.MapSlice{
				{Key: "unknownType", Value: yaml.MapSlice{
					{Key: "X", Value: "y"},
				}},
			},
			existing:    yaml.MapSlice{},
			currentFile: "x.yaml",
			wantTypes:   map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &OpenAPI{Components: tt.existing}
			urls := make(map[string]bool)

			mergeComponents(tt.nested, api, urls, tt.currentFile)

			for compType, wantKeys := range tt.wantTypes {
				comp, ok := getMapSliceValue(api.Components, compType).(yaml.MapSlice)
				if !ok {
					t.Fatalf("expected component type %q to exist, got %#v", compType, api.Components)
				}
				gotKeys := keys(comp)
				if !reflect.DeepEqual(gotKeys, wantKeys) {
					t.Errorf("component %q keys = %v, want %v", compType, gotKeys, wantKeys)
				}
			}
			for _, want := range tt.wantQueued {
				if !urls[want] {
					t.Errorf("expected %q to be queued, got %v", want, urls)
				}
			}
		})
	}
}

func TestMergeTopLevelComponentTypes(t *testing.T) {
	tests := []struct {
		name       string
		nested     yaml.MapSlice
		wantAdded  bool
		wantSchema string
	}{
		{
			name: "top-level schemas is merged",
			nested: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "User", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
				}},
			},
			wantAdded:  true,
			wantSchema: "User",
		},
		{
			name: "unknown top-level type is ignored",
			nested: yaml.MapSlice{
				{Key: "widgets", Value: yaml.MapSlice{
					{Key: "X", Value: "y"},
				}},
			},
			wantAdded: false,
		},
		{
			name:      "empty nested is a no-op",
			nested:    yaml.MapSlice{},
			wantAdded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &OpenAPI{}
			urls := make(map[string]bool)
			mergeTopLevelComponentTypes(tt.nested, api, urls, "x.yaml")

			if !tt.wantAdded {
				if len(api.Components) != 0 {
					t.Errorf("expected no components, got %#v", api.Components)
				}
				return
			}
			schemas, ok := getMapSliceValue(api.Components, "schemas").(yaml.MapSlice)
			if !ok {
				t.Fatalf("expected schemas to be added")
			}
			if getMapSliceValue(schemas, tt.wantSchema) == nil {
				t.Errorf("expected schema %q to be present", tt.wantSchema)
			}
		})
	}
}

func TestMergeNestedComponents(t *testing.T) {
	tests := []struct {
		name          string
		nested        yaml.MapSlice
		wantSchemas   []string
		wantResponses []string
	}{
		{
			name: "components-wrapped shape",
			nested: yaml.MapSlice{
				{Key: "components", Value: yaml.MapSlice{
					{Key: "schemas", Value: yaml.MapSlice{
						{Key: "User", Value: yaml.MapSlice{}},
					}},
				}},
			},
			wantSchemas: []string{"User"},
		},
		{
			name: "top-level shape",
			nested: yaml.MapSlice{
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "Order", Value: yaml.MapSlice{}},
				}},
			},
			wantSchemas: []string{"Order"},
		},
		{
			name: "both shapes present are both merged",
			nested: yaml.MapSlice{
				{Key: "components", Value: yaml.MapSlice{
					{Key: "responses", Value: yaml.MapSlice{
						{Key: "OK", Value: yaml.MapSlice{}},
					}},
				}},
				{Key: "schemas", Value: yaml.MapSlice{
					{Key: "Foo", Value: yaml.MapSlice{}},
				}},
			},
			wantSchemas:   []string{"Foo"},
			wantResponses: []string{"OK"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &OpenAPI{}
			urls := make(map[string]bool)
			mergeNestedComponents(tt.nested, api, urls, "x.yaml")

			if len(tt.wantSchemas) > 0 {
				schemas, _ := getMapSliceValue(api.Components, "schemas").(yaml.MapSlice)
				if got := keys(schemas); !reflect.DeepEqual(got, tt.wantSchemas) {
					t.Errorf("schemas = %v, want %v", got, tt.wantSchemas)
				}
			}
			if len(tt.wantResponses) > 0 {
				resps, _ := getMapSliceValue(api.Components, "responses").(yaml.MapSlice)
				if got := keys(resps); !reflect.DeepEqual(got, tt.wantResponses) {
					t.Errorf("responses = %v, want %v", got, tt.wantResponses)
				}
			}
		})
	}
}

func TestProcessSingleNestedFile(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		target        string
		wantErrText   string
		wantSchema    string
		wantProcessed bool
	}{
		{
			name: "successful merge marks url processed and adds schema",
			files: map[string]string{
				"schemas.yaml": `components:
  schemas:
    User:
      type: object
`,
			},
			target:        "schemas.yaml",
			wantSchema:    "User",
			wantProcessed: true,
		},
		{
			name:          "missing file returns error but still marks processed",
			target:        "missing.yaml",
			wantErrText:   "failed to read",
			wantProcessed: true,
		},
		{
			name: "malformed file returns error but still marks processed",
			files: map[string]string{
				"bad.yaml": "\t- : ][\n",
			},
			target:        "bad.yaml",
			wantErrText:   "failed to parse",
			wantProcessed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			url := filepath.Join(dir, tt.target)

			api := &OpenAPI{}
			urls := make(map[string]bool)
			processed := make(map[string]bool)

			err := processSingleNestedFile(url, api, urls, processed)

			if tt.wantProcessed && !processed[url] {
				t.Errorf("expected url marked processed")
			}
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrText)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			schemas, _ := getMapSliceValue(api.Components, "schemas").(yaml.MapSlice)
			if getMapSliceValue(schemas, tt.wantSchema) == nil {
				t.Errorf("expected schema %q, got %#v", tt.wantSchema, api.Components)
			}
		})
	}
}

func TestProcessNestedFiles(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		initialURLs []string
		wantErrText string
		wantSchemas []string
	}{
		{
			name: "single file merge",
			files: map[string]string{
				"a.yaml": `components:
  schemas:
    User:
      type: object
`,
			},
			initialURLs: []string{"a.yaml"},
			wantSchemas: []string{"User"},
		},
		{
			name: "transitive refs are discovered and merged",
			files: map[string]string{
				"a.yaml": `components:
  schemas:
    A:
      $ref: './b.yaml#/components/schemas/B'
`,
				"b.yaml": `components:
  schemas:
    B:
      type: object
`,
			},
			initialURLs: []string{"a.yaml"},
			wantSchemas: []string{"A", "B"},
		},
		{
			name: "duplicate merges do not blow up",
			files: map[string]string{
				"a.yaml": `components:
  schemas:
    A:
      type: object
`,
			},
			// Same URL appears twice - the second should be filtered by 'processed'.
			initialURLs: []string{"a.yaml", "a.yaml"},
			wantSchemas: []string{"A"},
		},
		{
			name:        "missing file propagates error",
			initialURLs: []string{"nope.yaml"},
			wantErrText: "failed to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}

			api := &OpenAPI{}
			urls := make(map[string]bool)
			for _, u := range tt.initialURLs {
				urls[filepath.Join(dir, u)] = true
			}

			err := processNestedFiles(urls, api)

			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrText)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			schemas, _ := getMapSliceValue(api.Components, "schemas").(yaml.MapSlice)
			gotKeys := keys(schemas)
			sort.Strings(gotKeys)
			wantKeys := append([]string(nil), tt.wantSchemas...)
			sort.Strings(wantKeys)
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Errorf("schemas = %v, want %v", gotKeys, wantKeys)
			}
		})
	}
}

// keys returns the list of MapSlice keys as strings, preserving order.
func keys(m yaml.MapSlice) []string {
	out := make([]string, 0, len(m))
	for _, item := range m {
		out = append(out, item.Key.(string))
	}
	return out
}
