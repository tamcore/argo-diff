package diff

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v3"
)

const (
	helmHookAnnotation = "helm.sh/hook"
	maxCommentSize     = 60000 // GitHub's limit is 65536, leave some buffer
)

// Resource represents a Kubernetes resource extracted from a YAML manifest
type Resource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string            `yaml:"name"`
		Namespace   string            `yaml:"namespace,omitempty"`
		Annotations map[string]string `yaml:"annotations,omitempty"`
	} `yaml:"metadata"`
	raw string
}

// GenerateDiff generates a formatted diff between base and head manifests
func GenerateDiff(baseManifests, headManifests []string, appName string) (string, error) {
	baseResources, err := parseManifests(baseManifests)
	if err != nil {
		return "", fmt.Errorf("parse base manifests: %w", err)
	}

	headResources, err := parseManifests(headManifests)
	if err != nil {
		return "", fmt.Errorf("parse head manifests: %w", err)
	}

	// Filter out helm hooks
	baseResources = filterHelmHooks(baseResources)
	headResources = filterHelmHooks(headResources)

	// Create resource maps for comparison
	baseMap := make(map[string]*Resource)
	for _, r := range baseResources {
		baseMap[r.key()] = r
	}

	headMap := make(map[string]*Resource)
	for _, r := range headResources {
		headMap[r.key()] = r
	}

	var diffs []string

	// Find modified and deleted resources
	for key, base := range baseMap {
		if head, exists := headMap[key]; exists {
			// Resource exists in both - check for changes
			if base.raw != head.raw {
				diff := generateResourceDiff(base, head)
				diffs = append(diffs, diff)
			}
		} else {
			// Resource deleted
			diff := fmt.Sprintf("<details>\n<summary>🗑️ Deleted: %s</summary>\n\n```yaml\n%s\n```\n</details>",
				base.key(), base.raw)
			diffs = append(diffs, diff)
		}
	}

	// Find new resources
	for key, head := range headMap {
		if _, exists := baseMap[key]; !exists {
			diff := fmt.Sprintf("<details>\n<summary>➕ Added: %s</summary>\n\n```yaml\n%s\n```\n</details>",
				head.key(), head.raw)
			diffs = append(diffs, diff)
		}
	}

	if len(diffs) == 0 {
		return fmt.Sprintf("## ✅ No changes for `%s`\n", appName), nil
	}

	result := fmt.Sprintf("## 📝 Changes for `%s`\n\n", appName)
	result += strings.Join(diffs, "\n\n")

	// Truncate if too large
	if len(result) > maxCommentSize {
		truncated := result[:maxCommentSize-200]
		result = truncated + "\n\n... (diff truncated due to size)"
	}

	return result, nil
}

// parseManifests parses YAML manifests into Resource structs
func parseManifests(manifests []string) ([]*Resource, error) {
	var resources []*Resource

	for _, manifest := range manifests {
		// Split YAML documents
		docs := strings.Split(manifest, "\n---\n")
		for _, doc := range docs {
			doc = strings.TrimSpace(doc)
			if doc == "" {
				continue
			}

			var r Resource
			if err := yaml.Unmarshal([]byte(doc), &r); err != nil {
				continue // Skip invalid YAML
			}

			// Skip empty resources
			if r.APIVersion == "" || r.Kind == "" || r.Metadata.Name == "" {
				continue
			}

			r.raw = doc
			resources = append(resources, &r)
		}
	}

	return resources, nil
}

// filterHelmHooks removes resources with helm hook annotations
func filterHelmHooks(resources []*Resource) []*Resource {
	filtered := make([]*Resource, 0, len(resources))
	for _, r := range resources {
		if _, isHook := r.Metadata.Annotations[helmHookAnnotation]; !isHook {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// key returns a unique key for the resource
func (r *Resource) key() string {
	if r.Metadata.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", r.APIVersion, r.Kind, r.Metadata.Namespace, r.Metadata.Name)
	}
	return fmt.Sprintf("%s/%s/%s", r.APIVersion, r.Kind, r.Metadata.Name)
}

// generateResourceDiff generates a unified diff for a single resource
func generateResourceDiff(base, head *Resource) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(base.raw, head.raw, false)

	// Generate unified diff format
	var buf bytes.Buffer
	lineNum := 1
	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		for i, line := range lines {
			if i == len(lines)-1 && line == "" {
				continue
			}
			switch d.Type {
			case diffmatchpatch.DiffDelete:
				buf.WriteString(fmt.Sprintf("-%s\n", line))
			case diffmatchpatch.DiffInsert:
				buf.WriteString(fmt.Sprintf("+%s\n", line))
			case diffmatchpatch.DiffEqual:
				buf.WriteString(fmt.Sprintf(" %s\n", line))
			}
			lineNum++
		}
	}

	return fmt.Sprintf("<details>\n<summary>🔄 Modified: %s</summary>\n\n```diff\n%s```\n</details>",
		head.key(), buf.String())
}

// SortResources sorts resources by kind and name for consistent ordering
func SortResources(resources []*Resource) {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Metadata.Name < resources[j].Metadata.Name
	})
}
