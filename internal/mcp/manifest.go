package mcp

import (
	"encoding/json"
	"sort"
)

// ToolManifest is the machine-readable tool surface published as a release
// artifact so agent-skills CI can lint skill-documented tool calls against
// the real schemas instead of drifting silently (issue: CI fitness function
// for skill-vs-tool-surface drift). One entry per distinct tool name; for
// submit_outcome, whose schema varies by TATARA_TOOL_PROFILE (contract D.1),
// enum values are the UNION across all seven profiles. That is a deliberate
// simplification (B1 scope: tool names + enum literals only, not per-profile
// shape) - a skill using a value valid for a different kind still won't be
// flagged, but a value valid for NO kind will be.
type ToolManifest struct {
	Tools []ToolManifestEntry `json:"tools"`
}

// ToolManifestEntry describes one tool's name and its top-level enum fields
// (action/kind/decision/verdict/state/...), each mapped to its allowed
// values. Fields with no enum constraint are omitted.
type ToolManifestEntry struct {
	Name  string              `json:"name"`
	Enums map[string][]string `json:"enums,omitempty"`
}

// GenerateToolManifest walks every Tool this binary can register - across all
// profiles, since the manifest is profile-agnostic - and extracts each tool's
// name and top-level enum constraints from its JSON schema.
func GenerateToolManifest() ToolManifest {
	byName := map[string]map[string]map[string]bool{} // tool -> field -> value set

	addTool := func(t Tool) {
		fields := byName[t.Name]
		if fields == nil {
			fields = map[string]map[string]bool{}
			byName[t.Name] = fields
		}
		for field, values := range extractEnums(t.Schema) {
			set := fields[field]
			if set == nil {
				set = map[string]bool{}
				fields[field] = set
			}
			for _, v := range values {
				set[v] = true
			}
		}
	}

	for _, t := range CodeTools() {
		addTool(t)
	}
	for _, t := range MemoryTools() {
		addTool(t)
	}
	for _, t := range SCMTools() {
		addTool(t)
	}
	for _, t := range PlatformTools() {
		addTool(t)
	}
	for profile := range outcomeSchemas {
		if t, ok := OutcomeTool(profile); ok {
			addTool(t)
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	manifest := ToolManifest{Tools: make([]ToolManifestEntry, 0, len(names))}
	for _, name := range names {
		fields := byName[name]
		fieldNames := make([]string, 0, len(fields))
		for f := range fields {
			fieldNames = append(fieldNames, f)
		}
		sort.Strings(fieldNames)

		entry := ToolManifestEntry{Name: name}
		if len(fieldNames) > 0 {
			entry.Enums = map[string][]string{}
			for _, f := range fieldNames {
				values := make([]string, 0, len(fields[f]))
				for v := range fields[f] {
					values = append(values, v)
				}
				sort.Strings(values)
				entry.Enums[f] = values
			}
		}
		manifest.Tools = append(manifest.Tools, entry)
	}
	return manifest
}

// extractEnums parses a tool's raw JSON schema and returns, for every
// top-level property with a string "enum" array, that field name mapped to
// its allowed values. Only top-level properties are inspected - per B1 scope,
// the manifest covers the literal `tool(field="value")` shape skills actually
// use, not nested object shapes like submit_outcome's issue.parent.
func extractEnums(schema json.RawMessage) map[string][]string {
	if len(schema) == 0 {
		return nil
	}
	var parsed struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return nil
	}
	out := map[string][]string{}
	for field, prop := range parsed.Properties {
		if len(prop.Enum) > 0 {
			out[field] = prop.Enum
		}
	}
	return out
}
