package diff

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v3"
)

const (
	helmHookAnnotation = "helm.sh/hook"
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
// Returns a DiffResult with structured information about the diff
func GenerateDiff(baseManifests, headManifests []string, appInfo *AppInfo) (*DiffResult, error) {
	result := &DiffResult{
		AppInfo:    appInfo,
		Diffs:      []string{},
		HasChanges: false,
	}

	baseResources, err := parseManifests(baseManifests)
	if err != nil {
		return nil, fmt.Errorf("parse base manifests: %w", err)
	}

	headResources, err := parseManifests(headManifests)
	if err != nil {
		return nil, fmt.Errorf("parse head manifests: %w", err)
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

	// Find modified and deleted resources
	for key, base := range baseMap {
		if head, exists := headMap[key]; exists {
			// Resource exists in both - check for changes
			if base.raw != head.raw {
				diff := generateResourceDiff(base, head)
				result.Diffs = append(result.Diffs, diff)
				result.HasChanges = true
			}
		} else {
			// Resource deleted
			diff := fmt.Sprintf("<details>\n<summary>🗑️ Deleted: %s</summary>\n\n```yaml\n%s\n```\n</details>",
				base.key(), base.raw)
			result.Diffs = append(result.Diffs, diff)
			result.HasChanges = true
		}
	}

	// Find new resources
	for key, head := range headMap {
		if _, exists := baseMap[key]; !exists {
			diff := fmt.Sprintf("<details>\n<summary>➕ Added: %s</summary>\n\n```yaml\n%s\n```\n</details>",
				head.key(), head.raw)
			result.Diffs = append(result.Diffs, diff)
			result.HasChanges = true
		}
	}

	return result, nil
}

// GenerateDiffLegacy generates a formatted diff between base and head manifests (legacy format)
func GenerateDiffLegacy(baseManifests, headManifests []string, appName string) (string, error) {
	appInfo := &AppInfo{Name: appName}
	result, err := GenerateDiff(baseManifests, headManifests, appInfo)
	if err != nil {
		return "", err
	}
	return FormatAppDiff(result), nil
}

// FormatAppDiff formats a single application's diff result as markdown
func FormatAppDiff(result *DiffResult) string {
	if result.ErrorMessage != "" {
		return fmt.Sprintf("### ⚠️ `%s`\n\n%s", result.AppInfo.Name, result.ErrorMessage)
	}

	if !result.HasChanges {
		return fmt.Sprintf("### ✅ No changes for `%s`\n", result.AppInfo.Name)
	}

	var sb strings.Builder

	// Header with app name
	sb.WriteString(fmt.Sprintf("### 📝 `%s`\n\n", result.AppInfo.Name))

	// Status and health line
	sb.WriteString(fmt.Sprintf("**Status:** %s %s | **Health:** %s %s\n\n",
		result.AppInfo.StatusEmoji(), result.AppInfo.Status,
		result.AppInfo.HealthEmoji(), result.AppInfo.Health))

	// ArgoCD link if available
	if url := result.AppInfo.ArgoURL(); url != "" {
		sb.WriteString(fmt.Sprintf("[View in ArgoCD](%s)\n\n", url))
	}

	// Diffs
	sb.WriteString(strings.Join(result.Diffs, "\n\n"))

	return sb.String()
}

// FormatReport formats a complete diff report as markdown
func FormatReport(report *DiffReport) string {
	var sb strings.Builder

	// Header
	sb.WriteString("# ArgoCD Diff Preview\n\n")

	// Summary
	sb.WriteString(fmt.Sprintf("**%d** of **%d** applications have changes\n\n",
		report.AppsWithDiffs, report.TotalApps))

	// Timestamp
	sb.WriteString(fmt.Sprintf("_Generated at %s_\n\n", report.Timestamp))

	// Workflow identifier (for comment management)
	sb.WriteString(fmt.Sprintf("<!-- argocd-diff-workflow: %s -->\n\n", report.WorkflowName))

	sb.WriteString("---\n\n")

	// Application diffs
	for i, result := range report.Results {
		sb.WriteString(FormatAppDiff(result))
		if i < len(report.Results)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}

	return sb.String()
}

// NewDiffReport creates a new diff report with metadata
func NewDiffReport(workflowName string, results []*DiffResult) *DiffReport {
	report := &DiffReport{
		WorkflowName: workflowName,
		Timestamp:    time.Now().UTC().Format("3:04PM MST, 2 Jan 2006"),
		TotalApps:    len(results),
		Results:      results,
	}

	for _, r := range results {
		if r.HasChanges {
			report.AppsWithDiffs++
		}
	}

	return report
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
