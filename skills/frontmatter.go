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
type claudeCodeFrontmatter struct { //nolint:govet // fieldalignment: field order is the on-disk YAML key order (yaml.Marshal follows struct declaration order), not packed for size. Inline, not a .golangci.yaml exclusions.rules entry, because fieldalignment's diagnostic text encodes struct-specific byte-count arithmetic and can't be pinned to a stable text: regex the way this repo's other suppressions can.
	Name        string `yaml:"name"`
	Description string `yaml:"description"`

	// AllowedTools is a single space-joined string, not a YAML list,
	// regardless of whether the source value was a YAML list or a
	// space-/comma-separated string — matching Claude Code's own canonical
	// multi-entry example rather than reproducing whichever source supplied
	// the value.
	AllowedTools string `yaml:"allowed-tools,omitempty"`

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

// claudeCodeFrontmatterCamelToKebab maps each field's camelCase spelling —
// how Registry's content.Frontmatter writes it — to its kebab-case spelling
// — how a real SKILL.md's inline frontmatter writes it — so the merge in
// renderSkillMarkdown can treat both as the same field regardless of which
// source they came from. This is a narrower concern than "which fields this
// CLI knows about": a key absent from this table (license, compatibility,
// metadata, model, context, agent, hooks, and any field this CLI has no
// struct field for) is spelled identically in both cases and needs no
// entry — it passes through the merge and the final render under its own
// single spelling, preserved rather than dropped (see renderSkillMarkdown's
// leftover-key handling).
var claudeCodeFrontmatterCamelToKebab = map[string]string{
	"allowedTools":           "allowed-tools",
	"disallowedTools":        "disallowed-tools",
	"argumentHint":           "argument-hint",
	"disableModelInvocation": "disable-model-invocation",
	"userInvocable":          "user-invocable",
}

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
// every other key — including ones this CLI has no dedicated struct field
// for — untouched; renderSkillMarkdown preserves those as their own
// top-level keys rather than dropping or relocating them.
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

	rendered := claudeCodeFrontmatter{
		Name:                   remoteName,
		Description:            description,
		AllowedTools:           resolvedAllowedTools(content.AllowedTools, merged["allowed-tools"]),
		License:                stringValue(merged["license"]),
		Compatibility:          stringValue(merged["compatibility"]),
		ArgumentHint:           stringValue(merged["argument-hint"]),
		DisableModelInvocation: content.DisableModelInvocation,
		UserInvocable:          content.UserInvocable,
		Model:                  stringValue(merged["model"]),
		Context:                stringValue(merged["context"]),
		Agent:                  stringValue(merged["agent"]),
		Hooks:                  mapValue(merged["hooks"]),
		Metadata:               mapValue(merged["metadata"]),
	}

	// Every field renderSkillMarkdown has just consumed, whether resolved
	// through its own dedicated rule (name, description,
	// disable-model-invocation, user-invocable) or read straight off merged
	// above, is removed here. Whatever remains is a real frontmatter field
	// this CLI has no dedicated struct field for (e.g. arguments,
	// disallowed-tools, or any future Claude Code field) — it must be
	// preserved as its own top-level key below, not dropped and not folded
	// into metadata: Claude Code doesn't act on metadata's contents, so a
	// field like arguments that drives real behavior would silently stop
	// working if relocated there.
	for _, k := range []string{
		"name", "description", "allowed-tools", "license", "compatibility", "argument-hint",
		"disable-model-invocation", "user-invocable", "model", "context", "agent", "hooks", "metadata",
	} {
		delete(merged, k)
	}

	yamlBytes, err := marshalFlowArrays(rendered)
	if err != nil {
		return "", fmt.Errorf("failed to marshal SKILL.md frontmatter for %s: %s", remoteName, err.Error())
	}

	var leftoverBytes []byte

	if len(merged) > 0 {
		leftoverBytes, err = marshalFlowArrays(merged)
		if err != nil {
			return "", fmt.Errorf("failed to marshal leftover SKILL.md frontmatter fields for %s: %s", remoteName, err.Error())
		}
	}

	return "---\n" + string(yamlBytes) + string(leftoverBytes) + "---\n\n" + strippedBody, nil
}

// marshalFlowArrays behaves like yaml.Marshal, except every YAML sequence
// (array) anywhere in v's tree — top-level or nested inside a map — is
// rendered in flow style ("[a, b]") rather than yaml.v3's default block
// style ("- a\n- b"), matching how real Claude Code frontmatter writes its
// own array-valued fields (e.g. allowed-tools's sibling disallowed-tools,
// or a skill's arguments).
func marshalFlowArrays(v any) ([]byte, error) {
	var node yaml.Node

	if err := node.Encode(v); err != nil {
		return nil, err
	}

	flowStyleSequences(&node)

	return yaml.Marshal(&node)
}

// flowStyleSequences recursively sets every yaml.SequenceNode in node's
// tree to yaml.FlowStyle, leaving every other node kind's style untouched.
func flowStyleSequences(node *yaml.Node) {
	if node.Kind == yaml.SequenceNode {
		node.Style = yaml.FlowStyle
	}

	for _, child := range node.Content {
		flowStyleSequences(child)
	}
}

// stringValue returns v as a string, or "" if v is nil or not a string.
func stringValue(v any) string {
	s, _ := v.(string)

	return s
}

// resolvedAllowedTools returns the final space-joined allowed-tools string
// to write to the rendered SKILL.md: fromContent, joined, if non-nil
// (content.AllowedTools — Registry/Chat's own opinion, including a
// deliberate empty list); otherwise fallback — merged's own "allowed-tools"
// value, from whichever of content.Frontmatter or the inline body
// frontmatter supplied it.
//
// A fallback that is already a plain string is used byte-for-byte, never
// split into tokens and rejoined: content.Frontmatter's own copy of
// allowedTools is always a real list by the time it reaches here (Registry
// validates and tokenizes any string form before storage, per AS-1820), so
// the only source that can ever supply a raw string here is a Chat-authored
// skill's own inline body frontmatter, entirely outside Registry's
// validation. Splitting that string on commas/whitespace and rejoining it
// with single spaces would silently corrupt any entry with its own
// internal comma or irregular spacing — e.g. "Bash(echo a, b)" becoming
// "Bash(echo a b)" — for no benefit, since the result is immediately
// turned back into a single string either way; the CLI has no consumer
// that ever needs the intermediate tokenized form.
//
// A fallback that is a []any (an actual YAML list) has no such risk and is
// joined with a single space. Any other type, or a genuinely absent key,
// resolves to "".
func resolvedAllowedTools(fromContent []string, fallback any) string {
	if fromContent != nil {
		return strings.Join(fromContent, " ")
	}

	switch v := fallback.(type) {
	case string:
		return v
	case []any:
		return strings.Join(stringSliceValue(v), " ")
	default:
		return ""
	}
}

// stringSliceValue returns v as a []string if v is a []any (dropping any
// non-string element), or nil for any other type, including a genuinely
// absent key.
func stringSliceValue(v any) []string {
	val, ok := v.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(val))

	for _, item := range val {
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
