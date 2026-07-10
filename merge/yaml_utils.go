package merge

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// readAndParseYAMLFile reads a YAML file from refPath and parses it into a
// yaml.MapSlice, preserving key order. Errors are returned as *MergeError
// so callers can report the path key and originating file that triggered
// the read.
func readAndParseYAMLFile(refPath, pathKey, currentFilePath string) (yaml.MapSlice, error) {
	data, err := os.ReadFile(refPath)
	if err != nil {
		return nil, &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Cannot read referenced file '%s'", refPath), Cause: err}
	}

	var nested yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
		return nil, &MergeError{File: refPath, Message: "Invalid YAML syntax", Cause: err}
	}

	return nested, nil
}

// readAndParseFile reads a YAML file from url and parses it into a
// yaml.MapSlice, preserving key order. Unlike readAndParseYAMLFile it
// returns wrapped fmt.Errorf values because callers do not have path-key
// context.
func readAndParseFile(url string) (yaml.MapSlice, error) {
	data, err := os.ReadFile(url)
	if err != nil {
		return nil, fmt.Errorf("failed to read '%s': %w", url, err)
	}

	var nested yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("failed to parse '%s': %w", url, err)
	}

	return nested, nil
}

// getMapSliceValue returns the value associated with key in m, or nil if
// the key is not present.
func getMapSliceValue(m yaml.MapSlice, key string) interface{} {
	for _, item := range m {
		if item.Key == key {
			return item.Value
		}
	}
	return nil
}

// setMapSliceValue sets the value for key in the yaml.MapSlice pointed to
// by m. If the key already exists its value is updated in place; otherwise
// a new MapItem is appended.
func setMapSliceValue(m *yaml.MapSlice, key string, value interface{}) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, yaml.MapItem{Key: key, Value: value})
}
