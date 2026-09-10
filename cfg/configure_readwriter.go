package cfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ConfigReadWriter reads and updates the CLI configuration as a YAML node
// tree, preserving fields and comments unrelated to the configured value.
type ConfigReadWriter struct { //nolint:govet // fieldalignment: keep path and existence metadata before the YAML payload.
	path    string
	existed bool
	node    yaml.Node
}

// NewConfigReadWriter returns a ConfigReadWriter for the existing config.yaml
// or config.yml in registryDir, or a new config.yaml when neither exists.
func NewConfigReadWriter(registryDir string) (ConfigReadWriter, error) {
	path, existed, err := resolveConfigPath(registryDir)
	if err != nil {
		return ConfigReadWriter{}, fmt.Errorf("failed to resolve config path: %s", err.Error())
	}

	rw := ConfigReadWriter{path: path, existed: existed}
	if !existed {
		rw.initializeEmptyDocument()

		return rw, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return ConfigReadWriter{}, fmt.Errorf("failed to read config file at %s: %s", path, err.Error())
	}

	if err = yaml.Unmarshal(content, &rw.node); err != nil {
		return ConfigReadWriter{}, fmt.Errorf("failed to parse config file at %s: %s", path, err.Error())
	}

	if len(rw.node.Content) == 0 {
		rw.initializeEmptyDocument()
		rw.node.Content[0].HeadComment = commentsFromEmptyDocument(content)
	}

	return rw, nil
}

// Get returns the scalar string at path, or an empty string when the path does
// not exist or did not exist when the reader was created.
func (rw ConfigReadWriter) Get(path []string) string {
	if !rw.existed {
		return ""
	}

	current := documentRoot(&rw.node)
	for _, segment := range path {
		current = mappingValue(current, segment)
		if current == nil {
			return ""
		}
	}

	if current.Kind != yaml.ScalarNode {
		return ""
	}

	return current.Value
}

// Set creates or updates the scalar string at path.
func (rw *ConfigReadWriter) Set(path []string, value string) {
	if len(path) == 0 {
		return
	}

	current := rw.ensureMappingDocument()

	for index, segment := range path {
		existing := mappingValue(current, segment)
		if index == len(path)-1 {
			if existing == nil {
				current.Content = append(current.Content, scalarNode(segment), scalarNode(value))
			} else {
				existing.Kind = yaml.ScalarNode
				existing.Tag = "!!str"
				existing.Value = value
				existing.Content = nil
			}

			return
		}

		if existing == nil {
			existing = mappingNode()
			current.Content = append(current.Content, scalarNode(segment), existing)
		} else if existing.Kind != yaml.MappingNode {
			existing.Kind = yaml.MappingNode
			existing.Tag = "!!map"
			existing.Value = ""
			existing.Content = nil
			existing.Style = 0
		}

		current = existing
	}
}

// Write atomically persists the YAML node tree to its resolved path.
func (rw ConfigReadWriter) Write() error {
	content, err := yaml.Marshal(&rw.node)
	if err != nil {
		return fmt.Errorf("failed to marshal config file contents: %s", err.Error())
	}

	registryDir := filepath.Dir(rw.path)
	if err = os.MkdirAll(registryDir, 0755); err != nil {
		return fmt.Errorf("failed to create Registry config directory at %s: %s", registryDir, err.Error())
	}

	tempFile, err := os.CreateTemp(registryDir, ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for config write: %s", err.Error())
	}

	tempPath := tempFile.Name()
	removeTemp := true

	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err = tempFile.Write(content); err != nil {
		_ = tempFile.Close()

		return fmt.Errorf("failed to write temp config file at %s: %s", tempPath, err.Error())
	}

	if err = tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp config file at %s: %s", tempPath, err.Error())
	}

	if err = os.Rename(tempPath, rw.path); err != nil {
		return fmt.Errorf("failed to move temp config file into place at %s: %s", rw.path, err.Error())
	}

	removeTemp = false

	return nil
}

func (rw *ConfigReadWriter) initializeEmptyDocument() {
	rw.node = yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{mappingNode()},
	}
}

func (rw *ConfigReadWriter) ensureMappingDocument() *yaml.Node {
	root := documentRoot(&rw.node)
	if root != nil && root.Kind == yaml.MappingNode {
		return root
	}

	rw.initializeEmptyDocument()

	return rw.node.Content[0]
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	if node.Kind != yaml.DocumentNode {
		return node
	}

	if len(node.Content) == 0 {
		return nil
	}

	return node.Content[0]
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}

	return nil
}

func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func commentsFromEmptyDocument(content []byte) string {
	comments := make([]string, 0)

	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			comments = append(comments, trimmed)
		}
	}

	return strings.Join(comments, "\n")
}
