package skills

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// claudeCodeFrontmatter is the fixed-field-order on-disk representation of a
// SKILL.md frontmatter block. A plain map would marshal in Go's randomized
// map iteration order; this struct makes yaml.Marshal's output deterministic
// and matches the real Claude Code spec's field order.
type claudeCodeFrontmatter struct { //nolint:govet // fieldalignment: field order is the on-disk YAML key order (yaml.Marshal follows struct declaration order), not packed for size
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	AllowedTools []string `yaml:"allowed-tools,omitempty"`

	License                string `yaml:"license,omitempty"`
	Compatibility          string `yaml:"compatibility,omitempty"`
	ArgumentHint           string `yaml:"argument-hint,omitempty"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation,omitempty"`

	// UserInvocable has no omitempty: false is the meaningful, non-default
	// value here (Claude Code's own default is true), so omitempty would
	// silently drop an explicit "not user-invocable" setting from the file.
	// This mirrors DisableModelInvocation, where false is the default and
	// omitting it is correct.
	UserInvocable bool `yaml:"user-invocable"`

	Model    string         `yaml:"model,omitempty"`
	Context  string         `yaml:"context,omitempty"`
	Agent    string         `yaml:"agent,omitempty"`
	Hooks    map[string]any `yaml:"hooks,omitempty"`
	Metadata map[string]any `yaml:"metadata,omitempty"`
}

var (
	// claudeCodeFrontmatterCamelToKebab maps each recognized SKILL.md
	// frontmatter field's canonical kebab-case key to the camelCase key
	// Registry's API uses for the same field. Keys not listed here (license,
	// compatibility, metadata, model, context, agent, hooks) are spelled
	// identically in both cases and need no entry.
	claudeCodeFrontmatterCamelToKebab = map[string]string{
		"allowedTools":           "allowed-tools",
		"argumentHint":           "argument-hint",
		"disableModelInvocation": "disable-model-invocation",
		"userInvocable":          "user-invocable",
	}

	// claudeCodeFrontmatterKnownKeys is the fixed set of canonical kebab-case
	// keys recognized as real SKILL.md frontmatter fields that participate in
	// the generic key-by-key merge in renderSkillMarkdown; anything else
	// found while merging is folded into `metadata` rather than dropped or
	// emitted as a bogus top-level key. "name", "description",
	// "disable-model-invocation", and "user-invocable" are deliberately
	// absent: renderSkillMarkdown resolves those 4 fields through their own
	// dedicated rules, never the generic merge, and deletes any copy of them
	// out of the merged map before this set is consulted — so they never
	// need an entry here.
	claudeCodeFrontmatterKnownKeys = map[string]bool{
		"allowed-tools": true, "license": true, "compatibility": true, "metadata": true,
		"argument-hint": true, "model": true, "context": true, "agent": true, "hooks": true,
	}
)

// splitFrontmatter splits a SKILL.md body into its optional leading YAML
// frontmatter block and the remaining markdown. A body with no leading
// "---" fence is normal — Registry-authored bodies are already
// stripped — and returns a nil frontmatter with a nil error. A body that
// starts with "---" but never closes the fence, or whose fenced content is
// not valid YAML or not a mapping, is a real error: a Chat-authored body can
// carry its own frontmatter block, so a malformed one is this skill's own
// data problem, not an absence to tolerate silently.
func splitFrontmatter(body string) (frontmatter map[string]any, rest string, err error) {
	trimmed := strings.TrimLeft(body, "\r\n\t ")

	if !strings.HasPrefix(trimmed, "---") {
		return nil, body, nil
	}

	after := trimmed[3:]

	if after != "" && after[0] != '\n' && !strings.HasPrefix(after, "\r\n") {
		// e.g. "---foo:" is not a fence line; a bare "\r" alone isn't a
		// recognized line terminator either (see indexClosingFence).
		return nil, body, nil
	}

	idx := indexClosingFence(after)
	if idx == -1 {
		return nil, "", errors.New("body starts with '---' but has no closing frontmatter fence")
	}

	var fm map[string]any

	if err = yaml.Unmarshal([]byte(after[:idx]), &fm); err != nil {
		return nil, "", fmt.Errorf("body frontmatter block is not valid YAML: %s", err.Error())
	}

	if fm == nil {
		return nil, "", errors.New("body frontmatter block did not parse to a YAML mapping")
	}

	return fm, strings.TrimLeft(after[idx+4:], "\n\r"), nil
}

// indexClosingFence returns the byte offset within s of the newline
// immediately preceding the first line that consists solely of "---",
// terminated by "\n", "\r\n", or the end of s — matching the
// commonly-accepted frontmatter convention, where the only valid line
// terminators are LF and CRLF. A bare "\r" not followed by "\n" does not
// count: that candidate line is skipped and scanning continues, since a
// lone CR is not a recognized line terminator (see
// .working-docs/spec/python-fm-parse.md for how Registry's own
// _parse_frontmatter diverges from this same convention). Returns -1 if s
// has no such line. s[idx+4:] is always exactly what follows the 3 closing
// dashes, since idx always points at a "\n" and the fence line itself is
// always "\n---" — 4 bytes.
func indexClosingFence(s string) int {
	for i := range len(s) {
		if s[i] != '\n' {
			continue
		}

		rest := s[i+1:]
		if !strings.HasPrefix(rest, "---") {
			continue
		}

		after := rest[3:]
		if after == "" || after[0] == '\n' || strings.HasPrefix(after, "\r\n") {
			return i
		}
	}

	return -1
}

// canonicalizeFrontmatterKeys renames every key with a
// claudeCodeFrontmatterCamelToKebab entry to its kebab-case form, leaving
// every other key — including genuinely unrecognized ones — untouched, to be
// sorted into known/unknown later against claudeCodeFrontmatterKnownKeys.
func canonicalizeFrontmatterKeys(fm map[string]any) map[string]any {
	if fm == nil {
		return nil
	}

	out := make(map[string]any, len(fm))

	for k, v := range fm {
		if kebab, ok := claudeCodeFrontmatterCamelToKebab[k]; ok {
			out[kebab] = v
		} else {
			out[k] = v
		}
	}

	return out
}

// renderSkillMarkdown reconstructs a complete on-disk SKILL.md for a skill
// fetched from the Registry get-skill-content endpoint, merging up to 3
// possible sources of frontmatter data — content.Frontmatter, content.Body's
// own inline frontmatter, and content's own top-level fields — in a fixed
// preference order. remoteName, not content.Name or any frontmatter-sourced
// name, always becomes the written "name:", since it is what actually
// determines the local folder name the real SKILL.md spec requires "name"
// to match.
func renderSkillMarkdown(content Content, remoteName string) (string, error) {
	inlineFrontmatter, strippedBody, err := splitFrontmatter(content.Body)
	if err != nil {
		return "", err
	}

	description := strings.TrimSpace(content.Description)
	if description == "" {
		description = strings.TrimSpace(stringValue(inlineFrontmatter["description"]))
	}

	if description == "" {
		return "", errors.New("resolved description is empty in both content.Description and the body's own inline frontmatter")
	}

	merged := make(map[string]any)

	for k, v := range canonicalizeFrontmatterKeys(inlineFrontmatter) {
		if v != nil {
			merged[k] = v
		}
	}

	for k, v := range canonicalizeFrontmatterKeys(content.Frontmatter) {
		if v != nil {
			merged[k] = v
		}
	}

	// name/description are resolved above; disable-model-invocation and
	// user-invocable always come from content's own top-level fields below.
	// None of the 4 belong to the generic key-by-key merge, so any copy the
	// merge picked up must be removed before folding leftover keys into
	// metadata, or they would be duplicated there as if unrecognized.
	delete(merged, "name")
	delete(merged, "description")
	delete(merged, "disable-model-invocation")
	delete(merged, "user-invocable")

	allowedTools := content.AllowedTools
	if allowedTools == nil {
		allowedTools = stringSliceValue(merged["allowed-tools"])
	}

	metadata := make(map[string]any)

	for k, v := range merged {
		if !claudeCodeFrontmatterKnownKeys[k] {
			metadata[k] = v
		}
	}

	if explicit, ok := merged["metadata"].(map[string]any); ok {
		for k, v := range explicit {
			metadata[k] = v
		}
	}

	if len(metadata) == 0 {
		metadata = nil
	}

	rendered := claudeCodeFrontmatter{
		Name:                   remoteName,
		Description:            description,
		AllowedTools:           allowedTools,
		License:                stringValue(merged["license"]),
		Compatibility:          stringValue(merged["compatibility"]),
		ArgumentHint:           stringValue(merged["argument-hint"]),
		DisableModelInvocation: content.DisableModelInvocation,
		UserInvocable:          content.UserInvocable,
		Model:                  stringValue(merged["model"]),
		Context:                stringValue(merged["context"]),
		Agent:                  stringValue(merged["agent"]),
		Hooks:                  mapValue(merged["hooks"]),
		Metadata:               metadata,
	}

	yamlBytes, err := yaml.Marshal(rendered)
	if err != nil {
		return "", fmt.Errorf("failed to marshal SKILL.md frontmatter for %s: %s", remoteName, err.Error())
	}

	return "---\n" + string(yamlBytes) + "---\n\n" + strippedBody, nil
}

// stringValue returns v as a string, or "" if v is nil or not a string.
func stringValue(v any) string {
	s, _ := v.(string)

	return s
}

// stringSliceValue converts a YAML/JSON-decoded generic slice ([]any) into
// a []string, dropping any non-string element. It returns nil for any other
// type, including a genuinely absent key.
func stringSliceValue(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(items))

	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

// mapValue returns v as a map[string]any, or nil if v is not one.
func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)

	return m
}
