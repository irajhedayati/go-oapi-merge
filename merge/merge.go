// Package merge resolves external $ref references in an OpenAPI YAML
// document and inlines them into a single, self-contained specification.
//
// It walks the input file, follows every "$ref: './other.yaml#/foo'"
// pointer, and rewrites the reference into a local one after copying
// the target definition into the merged document's components section.
//
// # Usage
//
//	err := merge.OapiYaml("api.yaml", "merged.yaml")
//
// # Ordering guarantees
//
// Output is deterministic: component types appear in OpenAPI-canonical
// order and keys within each type are sorted alphabetically.
package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

type MergeError struct {
	File    string
	Path    string
	Message string
	Cause   error
}

func (e *MergeError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.File != "" {
		b.WriteString(" (in ")
		b.WriteString(e.File)
		if e.Path != "" {
			b.WriteString(" at ")
			b.WriteString(e.Path)
		}
		b.WriteString(")")
	}
	return b.String()
}

func (e *MergeError) Unwrap() error {
	return e.Cause
}

type OpenAPI struct {
	OpenAPI    string        `yaml:"openapi"`
	Info       yaml.MapSlice `yaml:"info"`
	Servers    []interface{} `yaml:"servers,omitempty"`
	Paths      yaml.MapSlice `yaml:"paths"`
	Components yaml.MapSlice `yaml:"components,omitempty"`
	Security   []interface{} `yaml:"security,omitempty"`
	Tags       []interface{} `yaml:"tags,omitempty"`
}

// OapiYaml reads an OpenAPI document from inputFile, resolves any external $ref references, and writes the merged
// result to outputFile.
//
// It returns a *MergeError describing the offending file and JSON pointer path when resolution fails.
func OapiYaml(inputFile, outputFile string) error {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return &MergeError{File: inputFile, Message: "Failed to read input file", Cause: err}
	}

	var mainAPI OpenAPI
	if err := yaml.UnmarshalWithOptions(data, &mainAPI, yaml.UseOrderedMap()); err != nil {
		return &MergeError{File: inputFile, Message: "Invalid OpenAPI YAML structure", Cause: err}
	}

	if mainAPI.OpenAPI == "" {
		return &MergeError{File: inputFile, Message: "Missing required field 'openapi'"}
	}
	if len(mainAPI.Info) == 0 {
		return &MergeError{File: inputFile, Message: "Missing required field 'info'"}
	}

	urlsToParse := make(map[string]bool)
	if err := processPaths(&mainAPI.Paths, urlsToParse, inputFile); err != nil {
		return err
	}

	if err := processNestedFiles(urlsToParse, &mainAPI); err != nil {
		return err
	}

	sortComponents(&mainAPI.Components)

	data, err = yaml.MarshalWithOptions(&mainAPI, yaml.Indent(2), yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	return os.WriteFile(outputFile, data, 0644)
}

// processPaths iterates over each path entry in the provided YAML MapSlice and resolves
// any external $ref references found within path items. For each path item that contains
// a $ref pointing to an external file (i.e., not a local fragment reference starting with "#"),
// the function performs the following steps:
//  1. Validates the $ref format to ensure it contains a fragment identifier.
//  2. Resolves the referenced file path relative to the current file.
//  3. Registers the resolved file path in urlsToParse for further processing.
//  4. Reads and parses the referenced YAML file.
//  5. Navigates to the specific fragment within the parsed YAML structure.
//  6. Validates that the resolved fragment is a valid YAML MapSlice (path item).
//  7. Recursively collects any nested $ref URLs from the resolved path item.
//  8. Replaces the original $ref entry in the paths slice with the fully resolved path item.
//
// Parameters:
//   - paths: A pointer to a yaml.MapSlice representing the OpenAPI paths object.
//   - urlsToParse: A map used to track all external file references encountered during processing.
//   - currentFilePath: The file path of the YAML file currently being processed, used for
//     resolving relative $ref paths and for error reporting.
//
// Returns an error if any $ref is malformed, if a referenced file cannot be read or parsed,
// if navigation to the specified fragment fails, or if the resolved reference is not a valid
// path item. Returns nil if all path entries are processed successfully.
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

// extractExternalRef checks if the given path value contains an external $ref
// (i.e., a $ref that does not start with "#"). It returns the reference string
// and true if an external reference is found, or an empty string and false otherwise.
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

// resolvePathItemRef resolves a $ref string for a path item by splitting the reference
// into its file path and fragment components, resolving the file path relative to the
// current file, parsing the referenced YAML file, and extracting the fragment from the
// parsed content. It returns the resolved path item as a yaml.MapSlice, the resolved
// file path, and any error encountered during the process.
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

// splitRef splits a $ref string into its file path and fragment components using '#' as the delimiter.
// It expects the format "filePath#fragment" and returns an error if the '#' separator is missing.
// Parameters:
//   - refStr: the full $ref string to split
//   - pathKey: the current path key used for error reporting
//   - currentFilePath: the file path where the $ref was found, used for error reporting
//
// Returns a slice of two strings [filePath, fragment] or an error if the format is invalid.
func splitRef(refStr, pathKey, currentFilePath string) ([]string, error) {
	parts := strings.SplitN(refStr, "#", 2)
	if len(parts) < 2 {
		return nil, &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Invalid $ref format '%s': missing fragment", refStr)}
	}
	return parts, nil
}

// normalizeFragment ensures the fragment starts with a leading slash.
// If the fragment does not already begin with "/", one is prepended.
func normalizeFragment(fragment string) string {
	if !strings.HasPrefix(fragment, "/") {
		return "/" + fragment
	}
	return fragment
}

// readAndParseYAMLFile reads a YAML file from the given refPath and parses its contents into a yaml.MapSlice.
// It returns the parsed MapSlice or a MergeError if the file cannot be read or contains invalid YAML syntax.
// Parameters:
//   - refPath: the path to the YAML file to read and parse
//   - pathKey: the key path used for error reporting context
//   - currentFilePath: the path of the file currently being processed, used in error messages
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

// extractFragment navigates to and extracts a specific fragment from a nested YAML structure.
// It takes a yaml.MapSlice representing the nested structure, a fragment string indicating
// the target path within the structure, and a refPath string for error reporting purposes.
// Returns the extracted yaml.MapSlice at the specified fragment path, or an error if the
// navigation fails or the target is not a valid yaml.MapSlice.
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

// navigateToFragment traverses a nested yaml.MapSlice structure following the path
// specified by the fragment string. The fragment is expected to be a JSON Pointer
// (RFC 6901) style path, where each segment is separated by "/" characters.
// It returns the value found at the specified path, or an error if the path is invalid
// or a key is not found. The refPath parameter is used for error reporting to indicate
// which file the reference originated from.
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

// processNestedFiles iterates over all URLs queued in urlsToParse, reads and
// parses each file, and merges any OpenAPI component definitions it finds into
// mainAPI. Because a nested file may itself reference additional files via $ref,
// the function uses a worklist algorithm: newly discovered URLs are added to
// urlsToParse during merging, and the loop continues until every queued URL has
// been processed. Processing order within each iteration is sorted
// alphabetically to ensure deterministic output regardless of Go's randomized
// map iteration.
func processNestedFiles(urlsToParse map[string]bool, mainAPI *OpenAPI) error {
	componentTypes := []string{"schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks"}

	// Use a worklist so that files discovered transitively (e.g. a schema file
	// that $refs another schema file) are also processed.
	processed := make(map[string]bool)
	for {
		pending := collectPendingURLs(urlsToParse, processed)
		if len(pending) == 0 {
			break
		}
		// Sort so processing order is deterministic — Go map iteration is
		// randomized, which would otherwise make merge results (and thus the
		// final output) vary between runs when the same key is defined in
		// more than one file.
		sort.Strings(pending)

		for _, url := range pending {
			if err := processSingleNestedFile(url, mainAPI, componentTypes, urlsToParse, processed); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectPendingURLs returns a slice of URLs from urlsToParse that have not yet been processed.
// It iterates over the urlsToParse map and filters out any URLs that are present in the processed map.
func collectPendingURLs(urlsToParse map[string]bool, processed map[string]bool) []string {
	var pending []string
	for url := range urlsToParse {
		if !processed[url] {
			pending = append(pending, url)
		}
	}
	return pending
}

// processSingleNestedFile processes a single nested file referenced by the given URL.
// It marks the URL as processed, reads and parses the nested file, and merges its
// components into the main API. The urlsToParse map is updated with any new URLs
// discovered during the merge, and the processed map tracks already-handled URLs
// to prevent redundant processing.
func processSingleNestedFile(url string, mainAPI *OpenAPI, componentTypes []string, urlsToParse map[string]bool, processed map[string]bool) error {
	// Mark the URL as processed to avoid reprocessing it in future calls
	processed[url] = true

	// Read and parse the nested file at the given URL
	nested, err := readAndParseFile(url)
	if err != nil {
		return err
	}

	// Merge the components from the nested file into the main API
	mergeNestedComponents(nested, mainAPI, componentTypes, urlsToParse, url)
	return nil
}

// readAndParseFile reads a file from the given path and parses its YAML content.
// It returns a yaml.MapSlice preserving the order of keys as they appear in the file.
// Returns an error if the file cannot be read or if the content is not valid YAML.
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

// mergeNestedComponents processes a nested YAML structure and merges its components
// into the main OpenAPI specification. It first checks for a top-level "components"
// key in the nested structure and merges any found component maps. It then processes
// any top-level component types that may exist directly in the nested structure.
// Parameters:
//   - nested: the nested YAML map slice to process
//   - mainAPI: the main OpenAPI specification to merge components into
//   - componentTypes: list of component type keys to look for and merge
//   - urlsToParse: map of URLs that have been or need to be parsed
//   - url: the source URL of the nested structure being processed
func mergeNestedComponents(nested yaml.MapSlice, mainAPI *OpenAPI, componentTypes []string, urlsToParse map[string]bool, url string) {
	if nestedComponents := getMapSliceValue(nested, "components"); nestedComponents != nil {
		if compMap, ok := nestedComponents.(yaml.MapSlice); ok {
			mergeComponents(compMap, mainAPI, componentTypes, urlsToParse, url)
		}
	}

	mergeTopLevelComponentTypes(nested, mainAPI, componentTypes, urlsToParse, url)
}

// mergeTopLevelComponentTypes checks if any of the specified component types exist in the nested yaml.MapSlice.
// If a matching component type is found, it merges the components from the nested structure into the mainAPI.
// The function iterates over the provided componentTypes and breaks out of the loop after the first match,
// ensuring that mergeComponents is only called once even if multiple component types are present.
func mergeTopLevelComponentTypes(nested yaml.MapSlice, mainAPI *OpenAPI, componentTypes []string, urlsToParse map[string]bool, url string) {
	for _, ct := range componentTypes {
		if getMapSliceValue(nested, ct) != nil {
			mergeComponents(nested, mainAPI, componentTypes, urlsToParse, url)
			break
		}
	}
}

// mergeComponents merges the components from a nested (referenced) OpenAPI document into the main OpenAPI document.
// It first scans the nested components for any $ref references and adds them to the urlsToParse map for further processing.
// Then, for each component type (e.g., schemas, responses, parameters), it iterates over the nested components
// and appends any items that do not already exist in the main API's components, avoiding duplicate entries.
func mergeComponents(nestedComponents yaml.MapSlice, mainAPI *OpenAPI, componentTypes []string, urlsToParse map[string]bool, currentFilePath string) {
	findRefs(&nestedComponents, urlsToParse, currentFilePath)

	for _, compType := range componentTypes {
		nestedComp, ok := getMapSliceValue(nestedComponents, compType).(yaml.MapSlice)
		if !ok {
			continue
		}

		mainComp, _ := getMapSliceValue(mainAPI.Components, compType).(yaml.MapSlice)
		for _, item := range nestedComp {
			if getMapSliceValue(mainComp, item.Key.(string)) == nil {
				mainComp = append(mainComp, item)
			}
		}
		setMapSliceValue(&mainAPI.Components, compType, mainComp)
	}
}

// findRefs traverses a YAML map slice and processes all "$ref" keys found within it.
// For each "$ref" value that contains a "#" and does not start with "#" (indicating an external reference),
// it splits the reference into a file path and an anchor, resolves the file path relative to the
// current file, and adds the resolved URL to the urlsToParse map. The "$ref" value is then updated
// to contain only the anchor portion (e.g., "#/components/schemas/Example").
// For all other keys, it delegates processing to the processValue function.
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

// getMapSliceValue searches for a key in a yaml.MapSlice and returns its associated
// value if found. If the key does not exist in the slice, it returns nil.
func getMapSliceValue(m yaml.MapSlice, key string) interface{} {
	for _, item := range m {
		if item.Key == key {
			return item.Value
		}
	}
	return nil
}

// setMapSliceValue sets the value for the given key in the yaml.MapSlice pointed to
// by m. If the key already exists, its value is updated in place. If the key is not
// found, a new MapItem is appended to the slice.
func setMapSliceValue(m *yaml.MapSlice, key string, value interface{}) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, yaml.MapItem{Key: key, Value: value})
}

// resolveRef resolves a relative file path against the directory of the current file.
// If relativePath is empty or already an absolute path, it is returned unchanged.
// Otherwise, the result is the cleaned, absolute path obtained by joining the
// directory of currentFilePath with relativePath.
func resolveRef(relativePath, currentFilePath string) string {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return relativePath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), relativePath))
}

// sortComponents produces deterministic output by (a) ordering component
// types in the OpenAPI-canonical order and (b) alphabetically sorting the
// keys within each type. Any unknown component types are preserved and
// appended alphabetically after the canonical ones.
func sortComponents(components *yaml.MapSlice) {
	if components == nil || len(*components) == 0 {
		return
	}

	canonical := []string{"schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks"}
	order := make(map[string]int, len(canonical))
	for i, name := range canonical {
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
