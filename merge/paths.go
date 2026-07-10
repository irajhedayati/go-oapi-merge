package merge

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// processPaths iterates over every entry in the paths object and, for
// each entry whose value is an external $ref, replaces the reference
// with the fully resolved path item pulled from the target file.
// Discovered file paths are added to urlsToParse for later merging.
func processPaths(paths *yaml.MapSlice, urlsToParse map[string]bool, currentFilePath string) error {
	for i := range *paths {
		pathKey := (*paths)[i].Key.(string)
		pathValue := (*paths)[i].Value

		refStr, ok := extractExternalRef(pathValue)
		if !ok {
			continue
		}

		resolvedPathItem, refPath, err := resolvePathItemRef(refStr, pathKey, currentFilePath)
		if err != nil {
			return err
		}

		urlsToParse[refPath] = true
		findRefs(&resolvedPathItem, urlsToParse, refPath)
		(*paths)[i].Value = resolvedPathItem
	}
	return nil
}

// extractExternalRef inspects a path-item value and returns the $ref
// string when it points at an external file. Local (#/...) references
// are ignored — they need no resolution.
func extractExternalRef(pathValue interface{}) (string, bool) {
	pathMap, ok := pathValue.(yaml.MapSlice)
	if !ok {
		return "", false
	}

	refValue := getMapSliceValue(pathMap, "$ref")
	if refValue == nil {
		return "", false
	}

	refStr, ok := refValue.(string)
	if !ok || strings.HasPrefix(refStr, "#") {
		return "", false
	}

	return refStr, true
}

// resolvePathItemRef splits a "file#/fragment" reference, reads the
// target file, and returns the referenced path item along with the
// resolved absolute file path.
func resolvePathItemRef(refStr, pathKey, currentFilePath string) (yaml.MapSlice, string, error) {
	parts, err := splitRef(refStr, pathKey, currentFilePath)
	if err != nil {
		return nil, "", err
	}

	refPath := resolveRef(parts[0], currentFilePath)
	fragment := normalizeFragment(parts[1])

	nested, err := readAndParseYAMLFile(refPath, pathKey, currentFilePath)
	if err != nil {
		return nil, "", err
	}

	resolvedPathItem, err := extractFragment(nested, fragment, refPath)
	if err != nil {
		return nil, "", err
	}

	return resolvedPathItem, refPath, nil
}

// splitRef splits a $ref of the form "file#fragment" into its two parts,
// returning an error if the '#' separator is missing.
func splitRef(refStr, pathKey, currentFilePath string) ([]string, error) {
	parts := strings.SplitN(refStr, "#", 2)
	if len(parts) < 2 {
		return nil, &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Invalid $ref format '%s': missing fragment", refStr)}
	}
	return parts, nil
}

// normalizeFragment ensures the JSON-pointer fragment begins with '/'.
func normalizeFragment(fragment string) string {
	if !strings.HasPrefix(fragment, "/") {
		return "/" + fragment
	}
	return fragment
}

// extractFragment navigates to the given JSON-pointer fragment in the
// parsed YAML document and asserts the result is a yaml.MapSlice
// (as required for a path item).
func extractFragment(nested yaml.MapSlice, fragment, refPath string) (yaml.MapSlice, error) {
	current, err := navigateToFragment(nested, fragment, refPath)
	if err != nil {
		return nil, err
	}

	resolvedPathItem, ok := current.(yaml.MapSlice)
	if !ok {
		return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference target"}
	}

	return resolvedPathItem, nil
}

// navigateToFragment walks a JSON-pointer (RFC 6901 style) through a
// yaml.MapSlice, returning the value at the target path.
func navigateToFragment(nested yaml.MapSlice, fragment, refPath string) (interface{}, error) {
	var current interface{} = nested
	for _, part := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		if part == "" {
			continue
		}
		currentMap, ok := current.(yaml.MapSlice)
		if !ok {
			return nil, &MergeError{File: refPath, Path: fragment, Message: "Invalid reference structure"}
		}
		value := getMapSliceValue(currentMap, part)
		if value == nil {
			return nil, &MergeError{File: refPath, Path: fragment, Message: fmt.Sprintf("Key '%s' not found", part)}
		}
		current = value
	}
	return current, nil
}
