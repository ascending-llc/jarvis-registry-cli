package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantFM   map[string]any
		wantRest string
		wantErr  string
	}{
		{
			name:     "no leading fence at all",
			body:     "# Just markdown\n\nNo frontmatter here.",
			wantRest: "# Just markdown\n\nNo frontmatter here.",
		},
		{
			name:     "leading dashes that are not a fence line",
			body:     "---foo: bar\nrest of body",
			wantRest: "---foo: bar\nrest of body",
		},
		{
			name:     "valid frontmatter block",
			body:     "---\nname: foo\ndescription: bar\n---\n\nBody text.",
			wantFM:   map[string]any{"name": "foo", "description": "bar"},
			wantRest: "Body text.",
		},
		{
			name:     "leading whitespace and blank lines before the opening fence",
			body:     "\n\n  \n---\nname: foo\n---\nBody.",
			wantFM:   map[string]any{"name": "foo"},
			wantRest: "Body.",
		},
		{
			name:    "unclosed fence",
			body:    "---\nname: foo\nno closing fence",
			wantErr: "no closing frontmatter fence",
		},
		{
			name:    "invalid yaml inside the fence",
			body:    "---\nname: [unterminated\n---\nBody.",
			wantErr: "not valid YAML",
		},
		{
			// yaml.Unmarshal itself rejects a sequence decoded into a map
			// target, so this surfaces through the "not valid YAML" branch
			// rather than the "did not parse to a mapping" one, which is
			// reserved for YAML that decodes to a nil map (e.g. empty or
			// explicit `null` content).
			name:    "fence content is a YAML sequence, not a mapping",
			body:    "---\n- one\n- two\n---\nBody.",
			wantErr: "not valid YAML",
		},
		{
			name:    "empty fence content",
			body:    "---\n---\nBody.",
			wantErr: "did not parse to a YAML mapping",
		},
		{
			name:     "crlf line endings round-trip correctly",
			body:     "---\r\nname: foo\r\n---\r\n\r\nBody.",
			wantFM:   map[string]any{"name": "foo"},
			wantRest: "Body.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm, rest, err := splitFrontmatter(c.body)

			if c.wantErr != "" {
				require.Error(t, err, "splitFrontmatter should have returned an error")
				assert.Contains(t, err.Error(), c.wantErr, "error message should explain the parse failure")

				return
			}

			require.NoError(t, err, "splitFrontmatter should not have returned an error")
			assert.Equal(t, c.wantFM, fm, "parsed frontmatter mismatch")
			assert.Equal(t, c.wantRest, rest, "remaining body mismatch")
		})
	}
}

func TestCanonicalizeFrontmatterKeys(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want map[string]any
		name string
	}{
		{name: "nil map stays nil", in: nil, want: nil},
		{
			name: "camelCase keys with a kebab entry are renamed",
			in:   map[string]any{"allowedTools": []any{"Bash"}, "userInvocable": false},
			want: map[string]any{"allowed-tools": []any{"Bash"}, "user-invocable": false},
		},
		{
			name: "already-kebab keys are left alone",
			in:   map[string]any{"allowed-tools": []any{"Bash"}},
			want: map[string]any{"allowed-tools": []any{"Bash"}},
		},
		{
			name: "single-word and unrecognized keys are untouched",
			in:   map[string]any{"license": "MIT", "foo": "bar"},
			want: map[string]any{"license": "MIT", "foo": "bar"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, canonicalizeFrontmatterKeys(c.in))
		})
	}
}

func TestRenderSkillMarkdown(t *testing.T) {
	cases := []struct {
		run  func(t *testing.T)
		name string
	}{
		{
			name: "registry-authored body with camelCase content.Frontmatter renders kebab-case",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Id:          "id-1",
					Name:        "some-other-name",
					Description: "A registry skill.",
					Body:        "# Some Skill\n\nDoes a thing.",
					Frontmatter: map[string]any{
						"allowedTools":  []any{"Bash", "Read"},
						"license":       "MIT",
						"compatibility": "Claude Code >= 1.0",
					},
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "remote-name-1")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, body, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "remote-name-1", stringValue(fm["name"]))
				assert.Equal(t, "A registry skill.", stringValue(fm["description"]))
				assert.Equal(t, []string{"Bash", "Read"}, stringSliceValue(fm["allowed-tools"]))
				assert.Equal(t, "MIT", stringValue(fm["license"]))
				assert.Equal(t, "Claude Code >= 1.0", stringValue(fm["compatibility"]))
				assert.Equal(t, "# Some Skill\n\nDoes a thing.", body)

				_, hasCamelKey := fm["allowedTools"]
				assert.False(t, hasCamelKey, "camelCase key must not survive into the rendered output")
			},
		},
		{
			name: "chat-authored inline frontmatter with empty content.Frontmatter is stripped from the body, not duplicated",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body:          "---\nname: old-name-ignored\ndescription: 'Inline description.'\nallowed-tools:\n  - Write\nlicense: Apache-2.0\n---\n\nChat authored body content.",
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "chat-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, body, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "chat-skill", stringValue(fm["name"]))
				assert.Equal(t, "Inline description.", stringValue(fm["description"]))
				assert.Equal(t, []string{"Write"}, stringSliceValue(fm["allowed-tools"]))
				assert.Equal(t, "Apache-2.0", stringValue(fm["license"]))
				assert.Equal(t, "Chat authored body content.", body)
				assert.NotContains(t, body, "---", "the inline frontmatter block must not remain in the body")
			},
		},
		{
			name: "content.Frontmatter wins over inline body frontmatter on the same key",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Description:   "Top-level description.",
					Body:          "---\ndescription: 'irrelevant, content.Description is used instead'\nlicense: Apache-1.1-inline\ncompatibility: inline-compat\n---\n\nBody.",
					Frontmatter:   map[string]any{"license": "MIT-structured"},
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "collide-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "MIT-structured", stringValue(fm["license"]), "content.Frontmatter should win the collision")
				assert.Equal(t, "inline-compat", stringValue(fm["compatibility"]), "a key only present inline should still come through")
			},
		},
		{
			name: "disableModelInvocation/userInvocable always come from content's own fields, never merged",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body: "---\ndescription: d\ndisable-model-invocation: true\nuser-invocable: false\n---\n\nBody.",
					Frontmatter: map[string]any{
						"disableModelInvocation": false,
						"userInvocable":          true,
					},
					DisableModelInvocation: true,
					UserInvocable:          false,
				}

				rendered, err := renderSkillMarkdown(content, "flags-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				dmi, ok := fm["disable-model-invocation"].(bool)
				require.True(t, ok, "disable-model-invocation should be present since it is true (non-default)")
				assert.True(t, dmi)

				ui, ok := fm["user-invocable"].(bool)
				require.True(t, ok, "user-invocable should always be present")
				assert.False(t, ui)
			},
		},
		{
			name: "disable-model-invocation is omitted when false (the default)",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Description: "d", Body: "Body.", DisableModelInvocation: false, UserInvocable: true}

				rendered, err := renderSkillMarkdown(content, "default-flags-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				_, ok := fm["disable-model-invocation"]
				assert.False(t, ok, "disable-model-invocation should be omitted when false")
			},
		},
		{
			name: "user-invocable explicitly false is written, not omitted like a default",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Description: "d", Body: "Body.", UserInvocable: false}

				rendered, err := renderSkillMarkdown(content, "not-user-invocable-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				ui, ok := fm["user-invocable"]
				require.True(t, ok, "user-invocable must never be omitted, even when false")
				assert.Equal(t, false, ui)
			},
		},
		{
			name: "content.AllowedTools nil falls back to content.Frontmatter's allowedTools",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Description:   "d",
					Body:          "Body.",
					Frontmatter:   map[string]any{"allowedTools": []any{"Bash"}},
					AllowedTools:  nil,
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "fallback-structured-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, []string{"Bash"}, stringSliceValue(fm["allowed-tools"]))
			},
		},
		{
			name: "content.AllowedTools nil falls back to inline body frontmatter's allowed-tools when content.Frontmatter has none",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body:          "---\ndescription: d\nallowed-tools:\n  - Read\n  - Write\n---\n\nBody.",
					AllowedTools:  nil,
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "fallback-inline-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, []string{"Read", "Write"}, stringSliceValue(fm["allowed-tools"]))
			},
		},
		{
			name: "content.AllowedTools explicit empty list is used as-is, not treated as absent",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Description:   "d",
					Body:          "---\ndescription: irrelevant\nallowed-tools:\n  - Bash\n---\n\nBody.",
					Frontmatter:   map[string]any{"allowedTools": []any{"Read"}},
					AllowedTools:  []string{},
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "explicit-empty-tools-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				_, ok := fm["allowed-tools"]
				assert.False(t, ok, "an explicit empty list marshals to an omitted key, same as absence")
				assert.NotContains(t, rendered, "Bash", "must not have fallen back to content.Frontmatter's list")
				assert.NotContains(t, rendered, "Read", "must not have fallen back to the inline body's list")
			},
		},
		{
			name: "an unrecognized frontmatter key folds into metadata instead of being dropped or left top-level",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\ndescription: d\nfoo: bar\n---\n\nBody.", UserInvocable: true}

				rendered, err := renderSkillMarkdown(content, "unknown-key-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				_, topLevel := fm["foo"]
				assert.False(t, topLevel, "unrecognized key must not appear as a bogus top-level key")

				metadata := mapValue(fm["metadata"])
				require.NotNil(t, metadata, "unrecognized key should have been folded into metadata")
				assert.Equal(t, "bar", stringValue(metadata["foo"]))
			},
		},
		{
			name: "an empty resolved description fails to sync",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "No frontmatter, no description field.", Description: "   "}

				_, err := renderSkillMarkdown(content, "empty-desc-skill")
				require.Error(t, err, "renderSkillMarkdown should fail on an empty description")
				assert.Contains(t, err.Error(), "description")
			},
		},
		{
			name: "a whitespace-only inline description also counts as empty",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\ndescription: '   '\n---\n\nBody."}

				_, err := renderSkillMarkdown(content, "whitespace-desc-skill")
				require.Error(t, err, "renderSkillMarkdown should fail on a whitespace-only description")
				assert.Contains(t, err.Error(), "description")
			},
		},
		{
			name: "an unclosed frontmatter fence fails to render",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\ndescription: d\nno closing fence here"}

				_, err := renderSkillMarkdown(content, "unclosed-skill")
				require.Error(t, err, "renderSkillMarkdown should fail on an unclosed fence")
				assert.Contains(t, err.Error(), "no closing frontmatter fence")
			},
		},
		{
			name: "invalid YAML inside the fence fails to render",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\ndescription: [unterminated\n---\n\nBody."}

				_, err := renderSkillMarkdown(content, "bad-yaml-skill")
				require.Error(t, err, "renderSkillMarkdown should fail on invalid YAML")
				assert.Contains(t, err.Error(), "not valid YAML")
			},
		},
		{
			name: "written name always equals remoteName, even when content.Name differs",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Id: "id-9", Name: "totally-different-name", Description: "d", Body: "Body.", UserInvocable: true}

				rendered, err := renderSkillMarkdown(content, "actual-remote-name")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "actual-remote-name", stringValue(fm["name"]))
			},
		},
		{
			name: "an explicit null in content.Frontmatter does not shadow a real inline value",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body:          "---\ndescription: d\nlicense: FromInline\n---\n\nBody.",
					Frontmatter:   map[string]any{"license": nil},
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "nil-shadow-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "FromInline", stringValue(fm["license"]))
			},
		},
		{
			name: "an explicit null in the inline body frontmatter does not produce a nil top-level key",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\ndescription: d\nlicense: null\n---\n\nBody.", UserInvocable: true}

				rendered, err := renderSkillMarkdown(content, "inline-null-skip-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				_, ok := fm["license"]
				assert.False(t, ok, "an explicit null must not shadow to a nil top-level key")
			},
		},
		{
			name: "an explicit metadata mapping wins over a folded unrecognized key on collision",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body:          "---\ndescription: d\nfoo: from-unrecognized\nmetadata:\n  foo: from-explicit-metadata\n  bar: baz\n---\n\nBody.",
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "metadata-collision-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				metadata := mapValue(fm["metadata"])
				require.NotNil(t, metadata)
				assert.Equal(t, "from-explicit-metadata", stringValue(metadata["foo"]), "explicit metadata should win the collision")
				assert.Equal(t, "baz", stringValue(metadata["bar"]))
			},
		},
		{
			name: "the inline body's own name/description keys are not duplicated into metadata",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{Body: "---\nname: should-not-leak\ndescription: 'Inline description.'\n---\n\nBody.", UserInvocable: true}

				rendered, err := renderSkillMarkdown(content, "no-leak-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Nil(t, mapValue(fm["metadata"]), "name/description must not be folded into metadata")
			},
		},
		{
			name: "disable-model-invocation/user-invocable keys in inline frontmatter are not duplicated into metadata",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Body:                   "---\ndescription: d\ndisable-model-invocation: true\nuser-invocable: false\n---\n\nBody.",
					DisableModelInvocation: false,
					UserInvocable:          true,
				}

				rendered, err := renderSkillMarkdown(content, "flags-no-leak-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Nil(t, mapValue(fm["metadata"]), "disable-model-invocation/user-invocable must not be folded into metadata")
			},
		},
		{
			name: "license/compatibility/argument-hint/model/context/agent/hooks pass through from content.Frontmatter",
			run: func(t *testing.T) {
				t.Helper()

				content := Content{
					Description: "d",
					Body:        "Body.",
					Frontmatter: map[string]any{
						"license":       "MIT",
						"compatibility": "compat",
						"argumentHint":  "<query>",
						"model":         "claude-sonnet",
						"context":       "ctx",
						"agent":         "agent-x",
						"hooks":         map[string]any{"pre": "do-thing"},
					},
					UserInvocable: true,
				}

				rendered, err := renderSkillMarkdown(content, "passthrough-skill")
				require.NoError(t, err, "renderSkillMarkdown should succeed")

				fm, _, err := splitFrontmatter(rendered)
				require.NoError(t, err, "rendered output should itself be a valid SKILL.md")

				assert.Equal(t, "MIT", stringValue(fm["license"]))
				assert.Equal(t, "compat", stringValue(fm["compatibility"]))
				assert.Equal(t, "<query>", stringValue(fm["argument-hint"]))
				assert.Equal(t, "claude-sonnet", stringValue(fm["model"]))
				assert.Equal(t, "ctx", stringValue(fm["context"]))
				assert.Equal(t, "agent-x", stringValue(fm["agent"]))

				hooks := mapValue(fm["hooks"])
				require.NotNil(t, hooks)
				assert.Equal(t, "do-thing", stringValue(hooks["pre"]))
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}
