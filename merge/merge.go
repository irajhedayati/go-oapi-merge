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

	"github.com/goccy/go-yaml"
)

// OpenAPI is the subset of an OpenAPI 3 document this package cares
// about. Field order in the struct matches the canonical output order
// (openapi, info, servers, paths, components, security, tags), so the
// marshaled YAML always follows that layout regardless of the input.
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
