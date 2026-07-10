package merge

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestNormalizeFragment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already has leading slash", "/paths/users", "/paths/users"},
		{"missing leading slash gets one", "paths/users", "/paths/users"},
		{"empty string becomes single slash", "", "/"},
		{"root fragment is idempotent", "/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeFragment(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		wantOK   bool
		wantFile string
		wantFrag string
	}{
		{"valid ref", "./file.yaml#/foo", true, "./file.yaml", "/foo"},
		{"valid ref without leading dot", "file.yaml#/foo", true, "file.yaml", "/foo"},
		{"valid ref with empty file part", "#/foo", true, "", "/foo"},
		{"empty fragment portion is still valid split", "file.yaml#", true, "file.yaml", ""},
		{"missing hash is an error", "file.yaml", false, "", ""},
		{"empty string is an error", "", false, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := splitRef(tt.ref, "/paths/x", "api.yaml")
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if parts[0] != tt.wantFile || parts[1] != tt.wantFrag {
					t.Errorf("got [%q, %q], want [%q, %q]", parts[0], parts[1], tt.wantFile, tt.wantFrag)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var me *MergeError
			if !errors.As(err, &me) {
				t.Fatalf("err is not *MergeError: %T", err)
			}
			if !strings.Contains(me.Message, "Invalid $ref format") {
				t.Errorf("message = %q, want to contain %q", me.Message, "Invalid $ref format")
			}
		})
	}
}

func TestExtractExternalRef(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantRef string
		wantOK  bool
	}{
		{
			name:    "external file ref",
			value:   yaml.MapSlice{{Key: "$ref", Value: "./file.yaml#/foo"}},
			wantRef: "./file.yaml#/foo",
			wantOK:  true,
		},
		{
			name:   "local ref is skipped",
			value:  yaml.MapSlice{{Key: "$ref", Value: "#/components/schemas/User"}},
			wantOK: false,
		},
		{
			name:   "map without $ref",
			value:  yaml.MapSlice{{Key: "get", Value: yaml.MapSlice{}}},
			wantOK: false,
		},
		{
			name:   "empty mapslice",
			value:  yaml.MapSlice{},
			wantOK: false,
		},
		{
			name:   "value is not a MapSlice",
			value:  "not a map",
			wantOK: false,
		},
		{
			name:   "$ref is a non-string value",
			value:  yaml.MapSlice{{Key: "$ref", Value: 42}},
			wantOK: false,
		},
		{
			name:   "$ref value is nil",
			value:  yaml.MapSlice{{Key: "$ref", Value: nil}},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRef, gotOK := extractExternalRef(tt.value)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotRef != tt.wantRef {
				t.Errorf("ref = %q, want %q", gotRef, tt.wantRef)
			}
		})
	}
}

func TestNavigateToFragment(t *testing.T) {
	doc := yaml.MapSlice{
		{Key: "components", Value: yaml.MapSlice{
			{Key: "schemas", Value: yaml.MapSlice{
				{Key: "User", Value: yaml.MapSlice{
					{Key: "type", Value: "object"},
				}},
			}},
		}},
	}

	tests := []struct {
		name        string
		fragment    string
		wantMap     bool
		wantErrText string
	}{
		{"root traversal returns the doc itself", "/", true, ""},
		{"single-level key exists", "/components", true, ""},
		{"two-level key exists", "/components/schemas", true, ""},
		{"three-level key exists", "/components/schemas/User", true, ""},
		{"missing top-level key", "/nope", false, "Key 'nope' not found"},
		{"missing intermediate key", "/components/whoops", false, "Key 'whoops' not found"},
		{"walking into scalar is a structure error", "/components/schemas/User/type/toolong", false, "Invalid reference structure"},
		{"double slashes are skipped", "/components//schemas", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := navigateToFragment(doc, tt.fragment, "ref.yaml")

			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantMap {
				if _, ok := got.(yaml.MapSlice); !ok {
					t.Errorf("expected MapSlice, got %T", got)
				}
			}
		})
	}
}

func TestExtractFragment(t *testing.T) {
	doc := yaml.MapSlice{
		{Key: "components", Value: yaml.MapSlice{
			{Key: "schemas", Value: yaml.MapSlice{
				{Key: "User", Value: yaml.MapSlice{{Key: "type", Value: "object"}}},
			}},
			{Key: "count", Value: 42},
		}},
	}

	tests := []struct {
		name        string
		fragment    string
		wantErrText string
	}{
		{"target is a MapSlice", "/components/schemas/User", ""},
		{"target is a scalar", "/components/count", "Invalid reference target"},
		{"navigation fails", "/does/not/exist", "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractFragment(doc, tt.fragment, "ref.yaml")
			if tt.wantErrText == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrText)
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("err = %q, want to contain %q", err.Error(), tt.wantErrText)
			}
		})
	}
}

func TestResolvePathItemRef(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		refStr      string
		wantErrText string
		wantHasKey  string
	}{
		{
			name: "resolves external path item successfully",
			files: map[string]string{
				"paths.yaml": `users:
  get:
    responses:
      "200":
        description: OK
`,
			},
			refStr:     "./paths.yaml#/users",
			wantHasKey: "get",
		},
		{
			name:        "invalid ref format is rejected",
			refStr:      "no-hash",
			wantErrText: "Invalid $ref format",
		},
		{
			name:        "missing file is reported",
			refStr:      "./missing.yaml#/x",
			wantErrText: "Cannot read referenced file",
		},
		{
			name: "malformed target file is reported",
			files: map[string]string{
				"bad.yaml": "\t- : ][\n",
			},
			refStr:      "./bad.yaml#/x",
			wantErrText: "Invalid YAML syntax",
		},
		{
			name: "missing fragment key is reported",
			files: map[string]string{
				"paths.yaml": "other: {}\n",
			},
			refStr:      "./paths.yaml#/users",
			wantErrText: "Key 'users' not found",
		},
		{
			name: "scalar target is rejected as invalid",
			files: map[string]string{
				"paths.yaml": "users: hello\n",
			},
			refStr:      "./paths.yaml#/users",
			wantErrText: "Invalid reference target",
		},
		{
			name: "fragment without leading slash is normalized",
			files: map[string]string{
				"paths.yaml": `users:
  get:
    responses:
      "200":
        description: OK
`,
			},
			refStr:     "./paths.yaml#users",
			wantHasKey: "get",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			current := filepath.Join(dir, "api.yaml")

			got, gotPath, err := resolvePathItemRef(tt.refStr, "/paths/~1users", current)

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
			if getMapSliceValue(got, tt.wantHasKey) == nil {
				t.Errorf("resolved item missing expected key %q; got %#v", tt.wantHasKey, got)
			}
			if !strings.HasSuffix(gotPath, filepath.Base(gotPath)) {
				t.Errorf("resolved path %q is not a filename", gotPath)
			}
		})
	}
}

func TestProcessPaths(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		inputPaths  string
		wantErrText string
		verify      func(t *testing.T, paths yaml.MapSlice, urls map[string]bool)
	}{
		{
			name: "external ref is replaced inline and queued",
			files: map[string]string{
				"paths.yaml": `users:
  get:
    responses:
      "200":
        description: OK
`,
			},
			inputPaths: `/users:
  $ref: './paths.yaml#/users'
`,
			verify: func(t *testing.T, paths yaml.MapSlice, urls map[string]bool) {
				t.Helper()
				pathValue := getMapSliceValue(paths, "/users")
				pm, ok := pathValue.(yaml.MapSlice)
				if !ok {
					t.Fatalf("expected /users to be MapSlice, got %T", pathValue)
				}
				if getMapSliceValue(pm, "get") == nil {
					t.Errorf("expected inlined get operation, got %#v", pm)
				}
				var seen bool
				for u := range urls {
					if strings.HasSuffix(u, "paths.yaml") {
						seen = true
					}
				}
				if !seen {
					t.Errorf("expected paths.yaml to be queued, got %v", urls)
				}
			},
		},
		{
			name: "local ref is left untouched",
			inputPaths: `/users:
  $ref: '#/components/schemas/User'
`,
			verify: func(t *testing.T, paths yaml.MapSlice, urls map[string]bool) {
				t.Helper()
				pathValue := getMapSliceValue(paths, "/users")
				pm, _ := pathValue.(yaml.MapSlice)
				if v := getMapSliceValue(pm, "$ref"); v != "#/components/schemas/User" {
					t.Errorf("expected local ref preserved, got %v", v)
				}
				if len(urls) != 0 {
					t.Errorf("expected no urls queued, got %v", urls)
				}
			},
		},
		{
			name: "path items without $ref are left untouched",
			inputPaths: `/health:
  get:
    responses:
      "200":
        description: OK
`,
			verify: func(t *testing.T, paths yaml.MapSlice, urls map[string]bool) {
				t.Helper()
				pathValue := getMapSliceValue(paths, "/health")
				pm, _ := pathValue.(yaml.MapSlice)
				if getMapSliceValue(pm, "get") == nil {
					t.Errorf("expected get operation preserved")
				}
			},
		},
		{
			name: "malformed $ref format returns error",
			inputPaths: `/users:
  $ref: 'no-hash-here'
`,
			wantErrText: "Invalid $ref format",
		},
		{
			name: "missing referenced file returns error",
			inputPaths: `/users:
  $ref: './missing.yaml#/users'
`,
			wantErrText: "Cannot read referenced file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			current := filepath.Join(dir, "api.yaml")

			var paths yaml.MapSlice
			if err := yaml.UnmarshalWithOptions([]byte(tt.inputPaths), &paths, yaml.UseOrderedMap()); err != nil {
				t.Fatalf("test fixture parse: %v", err)
			}

			urls := make(map[string]bool)
			err := processPaths(&paths, urls, current)

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
			if tt.verify != nil {
				tt.verify(t, paths, urls)
			}
		})
	}
}
