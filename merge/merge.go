// Package merge resolves external $ref references in an OpenAPI YAML
// document and inlines them into a single, self-contained specification.
//
// It walks the input file, follows every "$ref: './other.yaml#/foo'"
// pointer, and rewrites the reference into a local one after copying
// the target definition into the merged document's components section.
//
// # Usage
//
//    err := merge.OapiYaml("api.yaml", "merged.yaml")
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
//   1. Validates the $ref format to ensure it contains a fragment identifier.
//   2. Resolves the referenced file path relative to the current file.
//   3. Registers the resolved file path in urlsToParse for further processing.
//   4. Reads and parses the referenced YAML file.
//   5. Navigates to the specific fragment within the parsed YAML structure.
//   6. Validates that the resolved fragment is a valid YAML MapSlice (path item).
//   7. Recursively collects any nested $ref URLs from the resolved path item.
//   8. Replaces the original $ref entry in the paths slice with the fully resolved path item.
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

        pathMap, ok := pathValue.(yaml.MapSlice)
        if !ok {
            continue
        }

        refValue := getMapSliceValue(pathMap, "$ref")
        if refValue == nil {
            continue
        }

        refStr, ok := refValue.(string)
        if !ok || strings.HasPrefix(refStr, "#") {
            continue
        }

        parts := strings.SplitN(refStr, "#", 2)
        if len(parts) < 2 {
            return &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Invalid $ref format '%s': missing fragment", refStr)}
        }

        refPath := resolveRef(parts[0], currentFilePath)
        urlsToParse[refPath] = true

        fragment := parts[1]
        if !strings.HasPrefix(fragment, "/") {
            fragment = "/" + fragment
        }

        data, err := os.ReadFile(refPath)
        if err != nil {
            return &MergeError{File: currentFilePath, Path: pathKey, Message: fmt.Sprintf("Cannot read referenced file '%s'", refPath), Cause: err}
        }

        var nested yaml.MapSlice
        if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
            return &MergeError{File: refPath, Message: "Invalid YAML syntax", Cause: err}
        }

        current, err := navigateToFragment(nested, fragment, refPath)
        if err != nil {
            return err
        }

        resolvedPathItem, ok := current.(yaml.MapSlice)
        if !ok {
            return &MergeError{File: refPath, Path: fragment, Message: "Invalid reference target"}
        }

        findRefs(&resolvedPathItem, urlsToParse, refPath)
        (*paths)[i].Value = resolvedPathItem
    }
    return nil
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
        // Collect all files that have been queued but not yet processed.
        var pending []string
        for url := range urlsToParse {
            if !processed[url] {
                pending = append(pending, url)
            }
        }
        if len(pending) == 0 {
            break
        }
        // Sort so processing order is deterministic — Go map iteration is
        // randomized, which would otherwise make merge results (and thus the
        // final output) vary between runs when the same key is defined in
        // more than one file.
        sort.Strings(pending)

        for _, url := range pending {
            processed[url] = true

            data, err := os.ReadFile(url)
            if err != nil {
                return fmt.Errorf("failed to read '%s': %w", url, err)
            }

            var nested yaml.MapSlice
            if err := yaml.UnmarshalWithOptions(data, &nested, yaml.UseOrderedMap()); err != nil {
                return fmt.Errorf("failed to parse '%s': %w", url, err)
            }

            if nestedComponents := getMapSliceValue(nested, "components"); nestedComponents != nil {
                if compMap, ok := nestedComponents.(yaml.MapSlice); ok {
                    mergeComponents(compMap, mainAPI, componentTypes, urlsToParse, url)
                }
            }

            for _, ct := range componentTypes {
                if getMapSliceValue(nested, ct) != nil {
                    mergeComponents(nested, mainAPI, componentTypes, urlsToParse, url)
                    break
                }
            }
        }
    }
    return nil
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
