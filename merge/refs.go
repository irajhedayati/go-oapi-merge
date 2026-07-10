package merge

import (
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// findRefs walks a yaml.MapSlice and rewrites every external $ref (one
// pointing at a separate file) into a local-only reference. The file
// portion is registered in urlsToParse so the caller can queue it for
// merging; the value in place is replaced with just the "#..." fragment.
// Nested MapSlice and []interface{} values are recursed via processValue.
func findRefs(api *yaml.MapSlice, urlsToParse map[string]bool, currentFilePath string) {
	for i := range *api {
		key := (*api)[i].Key.(string)
		value := (*api)[i].Value

		if key == "$ref" {
			if refStr, ok := value.(string); ok && strings.Contains(refStr, "#") && !strings.HasPrefix(refStr, "#") {
				parts := strings.SplitN(refStr, "#", 2)
				if urlsToParse != nil {
					urlsToParse[resolveRef(parts[0], currentFilePath)] = true
				}
				(*api)[i].Value = "#" + parts[1]
			}
		} else {
			processValue(value, urlsToParse, currentFilePath)
		}
	}
}

// processValue dispatches recursion for findRefs. Only container types
// (yaml.MapSlice and []interface{}) need to be descended into; scalar
// values cannot contain $ref keys.
func processValue(v interface{}, urlsToParse map[string]bool, currentFilePath string) {
	switch vt := v.(type) {
	case yaml.MapSlice:
		findRefs(&vt, urlsToParse, currentFilePath)
	case []interface{}:
		for _, item := range vt {
			processValue(item, urlsToParse, currentFilePath)
		}
	}
}

// resolveRef resolves a relative file path against the directory of the
// current file. Empty or absolute inputs are returned unchanged.
func resolveRef(relativePath, currentFilePath string) string {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return relativePath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), relativePath))
}
