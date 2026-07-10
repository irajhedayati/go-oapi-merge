package merge

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestGetMapSliceValue(t *testing.T) {
	sample := yaml.MapSlice{
		{Key: "a", Value: 1},
		{Key: "b", Value: "two"},
		{Key: "c", Value: nil},
		{Key: "d", Value: yaml.MapSlice{{Key: "nested", Value: true}}},
	}

	tests := []struct {
		name string
		m    yaml.MapSlice
		key  string
		want interface{}
	}{
		{"scalar int value", sample, "a", 1},
		{"scalar string value", sample, "b", "two"},
		// getMapSliceValue treats an explicit nil the same as absent by design.
		{"explicit nil value", sample, "c", nil},
		{"nested mapslice value", sample, "d", yaml.MapSlice{{Key: "nested", Value: true}}},
		{"missing key", sample, "zzz", nil},
		{"empty slice", yaml.MapSlice{}, "a", nil},
		{"nil slice", nil, "a", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMapSliceValue(tt.m, tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSetMapSliceValue(t *testing.T) {
	tests := []struct {
		name  string
		start yaml.MapSlice
		key   string
		value interface{}
		want  yaml.MapSlice
	}{
		{
			name:  "append to empty",
			start: yaml.MapSlice{},
			key:   "a",
			value: 1,
			want:  yaml.MapSlice{{Key: "a", Value: 1}},
		},
		{
			name:  "append to nonempty preserves order",
			start: yaml.MapSlice{{Key: "a", Value: 1}},
			key:   "b",
			value: 2,
			want:  yaml.MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: 2}},
		},
		{
			name:  "update existing key in place",
			start: yaml.MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: 2}},
			key:   "b",
			value: 99,
			want:  yaml.MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: 99}},
		},
		{
			name:  "update preserves position among siblings",
			start: yaml.MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: 2}, {Key: "c", Value: 3}},
			key:   "b",
			value: 99,
			want:  yaml.MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: 99}, {Key: "c", Value: 3}},
		},
		{
			name:  "update with nil value",
			start: yaml.MapSlice{{Key: "a", Value: 1}},
			key:   "a",
			value: nil,
			want:  yaml.MapSlice{{Key: "a", Value: nil}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.start
			setMapSliceValue(&got, tt.key, tt.value)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReadAndParseFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		createFile  bool
		wantKeys    []string
		wantErrText string
	}{
		{
			name:       "valid yaml preserves key order",
			content:    "z: 1\na: 2\nm: 3\n",
			createFile: true,
			wantKeys:   []string{"z", "a", "m"},
		},
		{
			name:        "missing file",
			createFile:  false,
			wantErrText: "failed to read",
		},
		{
			name:        "malformed yaml",
			content:     "\t- : ][\n",
			createFile:  true,
			wantErrText: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "in.yaml")
			if tt.createFile {
				writeFile(t, path, tt.content)
			}

			got, err := readAndParseFile(path)

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

			gotKeys := make([]string, 0, len(got))
			for _, item := range got {
				gotKeys = append(gotKeys, item.Key.(string))
			}
			if !reflect.DeepEqual(gotKeys, tt.wantKeys) {
				t.Errorf("keys = %v, want %v", gotKeys, tt.wantKeys)
			}
		})
	}
}

func TestReadAndParseYAMLFile(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		createFile     bool
		pathKey        string
		currentFile    string
		wantKeys       []string
		wantErrMessage string
		wantErrFile    string // matched with HasSuffix
		wantErrPath    string
	}{
		{
			name:        "valid yaml",
			content:     "x: 1\ny: 2\n",
			createFile:  true,
			pathKey:     "/paths",
			currentFile: "api.yaml",
			wantKeys:    []string{"x", "y"},
		},
		{
			name:           "missing file annotates with pathKey and currentFile",
			createFile:     false,
			pathKey:        "/paths/~1users",
			currentFile:    "api.yaml",
			wantErrMessage: "Cannot read referenced file",
			wantErrFile:    "api.yaml",
			wantErrPath:    "/paths/~1users",
		},
		{
			name:           "malformed yaml annotates with the referenced file",
			content:        "\t- : ][\n",
			createFile:     true,
			pathKey:        "/paths/~1x",
			currentFile:    "api.yaml",
			wantErrMessage: "Invalid YAML syntax",
			wantErrFile:    "in.yaml", // target file name, not currentFile
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "in.yaml")
			if tt.createFile {
				writeFile(t, target, tt.content)
			}

			got, err := readAndParseYAMLFile(target, tt.pathKey, tt.currentFile)

			if tt.wantErrMessage != "" {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var me *MergeError
				if !errors.As(err, &me) {
					t.Fatalf("err = %v (%T), want *MergeError", err, err)
				}
				if !strings.Contains(me.Message, tt.wantErrMessage) {
					t.Errorf("message = %q, want to contain %q", me.Message, tt.wantErrMessage)
				}
				if tt.wantErrFile != "" && !strings.HasSuffix(me.File, tt.wantErrFile) {
					t.Errorf("file = %q, want suffix %q", me.File, tt.wantErrFile)
				}
				if tt.wantErrPath != "" && me.Path != tt.wantErrPath {
					t.Errorf("path = %q, want %q", me.Path, tt.wantErrPath)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotKeys := make([]string, 0, len(got))
			for _, item := range got {
				gotKeys = append(gotKeys, item.Key.(string))
			}
			if !reflect.DeepEqual(gotKeys, tt.wantKeys) {
				t.Errorf("keys = %v, want %v", gotKeys, tt.wantKeys)
			}
		})
	}
}
