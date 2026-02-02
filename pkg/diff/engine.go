package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
		manifest = strings.TrimSpace(manifest)
		if manifest == "" {
			continue
		}

		// Check if this is JSON (ArgoCD API returns JSON manifests)
		if strings.HasPrefix(manifest, "{") {
			// Convert JSON to YAML for better diff readability
			yamlManifest, err := jsonToYAML(manifest)
			if err != nil {
				continue // Skip if conversion fails
			}
			manifest = yamlManifest
		}

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

// jsonToYAML converts a JSON string to YAML format
func jsonToYAML(jsonStr string) (string, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", err
	}

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(yamlBytes), nil
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
	// Generate line-based unified diff
	baseLines := strings.Split(base.raw, "\n")
	headLines := strings.Split(head.raw, "\n")

	// Create filename for the diff header
	filename := fmt.Sprintf("%s_%s_%s.yaml",
		base.Metadata.Namespace, base.Metadata.Name, base.Kind)
	if base.Metadata.Namespace == "" {
		filename = fmt.Sprintf("%s_%s.yaml", base.Metadata.Name, base.Kind)
	}

	diff := generateUnifiedDiff(baseLines, headLines, filename, 3) // 3 lines of context

	return fmt.Sprintf("<details open>\n<summary>===== %s =====</summary>\n\n```diff\n%s```\n</details>",
		head.key(), diff)
}

// generateUnifiedDiff creates a unified diff between two sets of lines with context
// Produces proper unified diff format with --- +++ headers and @@ hunk headers
func generateUnifiedDiff(oldLines, newLines []string, filename string, contextLines int) string {
	// Use a simple line-by-line diff algorithm
	// This produces cleaner output than character-based diffing

	// Find longest common subsequence-based diff
	type diffLine struct {
		text    string
		change  byte // ' ' = same, '-' = deleted, '+' = added
		oldLine int  // 1-based line number in old file (0 if not applicable)
		newLine int  // 1-based line number in new file (0 if not applicable)
	}

	// Simple O(n*m) LCS-based diff
	m, n := len(oldLines), len(newLines)

	// Build LCS table
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else {
				if lcs[i-1][j] > lcs[i][j-1] {
					lcs[i][j] = lcs[i-1][j]
				} else {
					lcs[i][j] = lcs[i][j-1]
				}
			}
		}
	}

	// Backtrack to build diff with line numbers
	var result []diffLine
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append([]diffLine{{text: oldLines[i-1], change: ' ', oldLine: i, newLine: j}}, result...)
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			result = append([]diffLine{{text: newLines[j-1], change: '+', oldLine: 0, newLine: j}}, result...)
			j--
		} else if i > 0 {
			result = append([]diffLine{{text: oldLines[i-1], change: '-', oldLine: i, newLine: 0}}, result...)
			i--
		}
	}

	// If no changes, return empty
	hasChanges := false
	for _, line := range result {
		if line.change != ' ' {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	// Generate unified diff output with proper headers and hunks
	var buf bytes.Buffer
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000000000 -0700")

	// Write file headers
	buf.WriteString(fmt.Sprintf("--- %s\t%s\n", filename, timestamp))
	buf.WriteString(fmt.Sprintf("+++ %s\t%s\n", filename, timestamp))

	// Find hunks (groups of changes with context)
	type hunk struct {
		startIdx int
		endIdx   int
	}

	var hunks []hunk
	inHunk := false
	hunkStart := 0

	for idx, line := range result {
		isChange := line.change != ' '
		nearChange := false

		// Check if within contextLines of a change
		for checkIdx := idx - contextLines; checkIdx <= idx+contextLines; checkIdx++ {
			if checkIdx >= 0 && checkIdx < len(result) && result[checkIdx].change != ' ' {
				nearChange = true
				break
			}
		}

		if isChange || nearChange {
			if !inHunk {
				hunkStart = idx
				inHunk = true
			}
		} else {
			if inHunk {
				hunks = append(hunks, hunk{startIdx: hunkStart, endIdx: idx - 1})
				inHunk = false
			}
		}
	}
	// Close final hunk if still open
	if inHunk {
		hunks = append(hunks, hunk{startIdx: hunkStart, endIdx: len(result) - 1})
	}

	// Output each hunk
	for _, h := range hunks {
		// Calculate line numbers for hunk header
		oldStart, oldCount := 0, 0
		newStart, newCount := 0, 0

		for idx := h.startIdx; idx <= h.endIdx; idx++ {
			line := result[idx]
			switch line.change {
			case ' ':
				if oldStart == 0 && line.oldLine > 0 {
					oldStart = line.oldLine
				}
				if newStart == 0 && line.newLine > 0 {
					newStart = line.newLine
				}
				oldCount++
				newCount++
			case '-':
				if oldStart == 0 && line.oldLine > 0 {
					oldStart = line.oldLine
				}
				oldCount++
			case '+':
				if newStart == 0 && line.newLine > 0 {
					newStart = line.newLine
				}
				newCount++
			}
		}

		// Default to line 1 if we couldn't determine
		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}

		// Write hunk header
		buf.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))

		// Write hunk content
		for idx := h.startIdx; idx <= h.endIdx; idx++ {
			line := result[idx]
			buf.WriteString(fmt.Sprintf("%c%s\n", line.change, line.text))
		}
	}

	return buf.String()
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
