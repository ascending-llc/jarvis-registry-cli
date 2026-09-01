package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// claudeCodeFrontmatter is the fixed-field-order on-disk representation of a
// SKILL.md frontmatter block. A plain map would marshal in Go's randomized
// map iteration order; this struct makes yaml.Marshal's output deterministic
// and matches the real Claude Code spec's field order.
type claudeCodeFrontmatter struct { //nolint:govet // fieldalignment: field order is the on-disk YAML key order (yaml.Marshal follows struct declaration order), not packed for size
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

	allowedTools := content.AllowedTools
	if allowedTools == nil {
		allowedTools = stringSliceValue(merged["allowed-tools"])
	}

	rendered := claudeCodeFrontmatter{
		Name:                   remoteName,
		Description:            description,
		AllowedTools:           strings.Join(allowedTools, " "),
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

	yamlBytes, err := yaml.Marshal(rendered)
	if err != nil {
		return "", fmt.Errorf("failed to marshal SKILL.md frontmatter for %s: %s", remoteName, err.Error())
	}

	var leftoverBytes []byte

	if len(merged) > 0 {
		leftoverBytes, err = yaml.Marshal(merged)
		if err != nil {
			return "", fmt.Errorf("failed to marshal leftover SKILL.md frontmatter fields for %s: %s", remoteName, err.Error())
		}
	}

	return "---\n" + string(yamlBytes) + string(leftoverBytes) + "---\n\n" + strippedBody, nil
}

// stringValue returns v as a string, or "" if v is nil or not a string.
func stringValue(v any) string {
	s, _ := v.(string)

	return s
}

// stringSliceValue converts a YAML/JSON-decoded value into a []string: a
// []any (dropping any non-string element), or the Agent Skills spec's own
// canonical space-/comma-separated string form (e.g. "Bash Read, Write"),
// split on any run of commas/whitespace — mirroring AS-1820's own
// _split_allowed_tools Pydantic validator, so both systems accept the same
// input shapes for the same reason. Returns nil for any other type,
// including a genuinely absent key.
func stringSliceValue(v any) []string {
	switch val := v.(type) {
	case []any:
		out := make([]string, 0, len(val))

		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}

		return out
	case string:
		return strings.FieldsFunc(val, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		})
	default:
		return nil
	}
}

// mapValue returns v as a map[string]any, or nil if v is not one.
func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)

	return m
}
