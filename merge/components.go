package merge

import (
	"sort"

	"github.com/goccy/go-yaml"
)

// componentTypes lists every OpenAPI 3 component bucket in canonical
// order. It's the shared source of truth for both merge order and sort
// order.
var componentTypes = []string{"schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks"}

// processNestedFiles drains a worklist of referenced files, merging any
// components they define into mainAPI. Newly discovered $refs are queued
// on urlsToParse as files are processed, and the loop continues until
// the queue is empty. Each iteration sorts pending URLs so results are
// deterministic despite Go's randomized map iteration.
func processNestedFiles(urlsToParse map[string]bool, mainAPI *OpenAPI) error {
	processed := make(map[string]bool)
	for {
		pending := collectPendingURLs(urlsToParse, processed)
		if len(pending) == 0 {
			break
		}
		sort.Strings(pending)

		for _, url := range pending {
			if err := processSingleNestedFile(url, mainAPI, urlsToParse, processed); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectPendingURLs returns the queued URLs that have not been processed yet.
func collectPendingURLs(urlsToParse, processed map[string]bool) []string {
	var pending []string
	for url := range urlsToParse {
		if !processed[url] {
			pending = append(pending, url)
		}
	}
	return pending
}

// processSingleNestedFile reads one queued file, marks it as processed,
// and merges the components it defines into mainAPI. Any new external
// $refs discovered during the merge are added to urlsToParse.
func processSingleNestedFile(url string, mainAPI *OpenAPI, urlsToParse, processed map[string]bool) error {
	processed[url] = true

	nested, err := readAndParseFile(url)
	if err != nil {
		return err
	}

	mergeNestedComponents(nested, mainAPI, urlsToParse, url)
	return nil
}

// mergeNestedComponents handles both shapes a referenced file may take:
// a top-level "components:" key (the canonical shape) or a file whose
// root already contains component-type keys directly (schemas:,
// responses:, ...).
func mergeNestedComponents(nested yaml.MapSlice, mainAPI *OpenAPI, urlsToParse map[string]bool, url string) {
	if nestedComponents := getMapSliceValue(nested, "components"); nestedComponents != nil {
		if compMap, ok := nestedComponents.(yaml.MapSlice); ok {
			mergeComponents(compMap, mainAPI, urlsToParse, url)
		}
	}

	mergeTopLevelComponentTypes(nested, mainAPI, urlsToParse, url)
}

// mergeTopLevelComponentTypes handles files whose root is itself a
// component bucket (e.g. a schema file that starts with "schemas:").
// It merges once if any canonical component key is present at the top
// level of nested.
func mergeTopLevelComponentTypes(nested yaml.MapSlice, mainAPI *OpenAPI, urlsToParse map[string]bool, url string) {
	for _, ct := range componentTypes {
		if getMapSliceValue(nested, ct) != nil {
			mergeComponents(nested, mainAPI, urlsToParse, url)
			break
		}
	}
}

// mergeComponents rewrites external $refs to local ones and appends any
// component definitions from nestedComponents into mainAPI.Components,
// skipping keys that already exist (first definition wins).
func mergeComponents(nestedComponents yaml.MapSlice, mainAPI *OpenAPI, urlsToParse map[string]bool, currentFilePath string) {
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
