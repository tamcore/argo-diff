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

// jsonToYAML converts a JSON string to YAML format with reasonable line width
func jsonToYAML(jsonStr string) (string, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(data); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
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
// Uses memory-efficient Myers diff algorithm instead of LCS table
func generateUnifiedDiff(oldLines, newLines []string, filename string, contextLines int) string {
	// Use a more memory-efficient approach: stream-based diff with hash comparison
	// First, quickly check if files are identical using a rolling comparison
	if len(oldLines) == len(newLines) {
		identical := true
		for i := range oldLines {
			if oldLines[i] != newLines[i] {
				identical = false
				break
			}
		}
		if identical {
			return ""
		}
	}

	// Hash function for line comparison
	hashLine := func(s string) uint64 {
		var h uint64 = 14695981039346656037 // FNV-1a offset basis
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211 // FNV-1a prime
		}
		return h
	}

	// Create hash maps for old and new lines
	oldHashes := make(map[uint64][]int) // hash -> line numbers (0-based)
	for i, line := range oldLines {
		h := hashLine(line)
		oldHashes[h] = append(oldHashes[h], i)
	}

	// Use patience diff-inspired approach: find unique matching lines as anchors
	// This is more memory efficient than full LCS for large files
	type match struct {
		oldIdx int
		newIdx int
	}
	var anchors []match

	// Find matching lines (using hash, then verify)
	usedOld := make(map[int]bool)
	for newIdx, line := range newLines {
		h := hashLine(line)
		if oldIdxs, ok := oldHashes[h]; ok {
			for _, oldIdx := range oldIdxs {
				if !usedOld[oldIdx] && oldLines[oldIdx] == line {
					anchors = append(anchors, match{oldIdx, newIdx})
					usedOld[oldIdx] = true
					break
				}
			}
		}
	}

	// Sort anchors by old index to get proper ordering
	sort.Slice(anchors, func(i, j int) bool {
		return anchors[i].oldIdx < anchors[j].oldIdx
	})

	// Find longest increasing subsequence of new indices (to handle reorders)
	// This gives us the best matching sequence
	var lis []match
	if len(anchors) > 0 {
		// Simple O(n²) LIS - good enough for reasonable anchor counts
		dp := make([]int, len(anchors))
		parent := make([]int, len(anchors))
		for i := range dp {
			dp[i] = 1
			parent[i] = -1
		}

		maxLen, maxIdx := 1, 0
		for i := 1; i < len(anchors); i++ {
			for j := 0; j < i; j++ {
				if anchors[j].newIdx < anchors[i].newIdx && dp[j]+1 > dp[i] {
					dp[i] = dp[j] + 1
					parent[i] = j
				}
			}
			if dp[i] > maxLen {
				maxLen = dp[i]
				maxIdx = i
			}
		}

		// Reconstruct LIS
		lisIdxs := make([]int, maxLen)
		for i, idx := maxLen-1, maxIdx; i >= 0; i-- {
			lisIdxs[i] = idx
			idx = parent[idx]
		}
		for _, idx := range lisIdxs {
			lis = append(lis, anchors[idx])
		}
	}

	// Generate diff lines from the matching sequence
	type diffLine struct {
		text    string
		change  byte // ' ' = same, '-' = deleted, '+' = added
		oldLine int  // 1-based line number in old file (0 if not applicable)
		newLine int  // 1-based line number in new file (0 if not applicable)
	}

	var result []diffLine
	oldIdx, newIdx := 0, 0

	for _, m := range lis {
		// Emit deletions from oldIdx to m.oldIdx
		for oldIdx < m.oldIdx {
			result = append(result, diffLine{
				text:    oldLines[oldIdx],
				change:  '-',
				oldLine: oldIdx + 1,
			})
			oldIdx++
		}
		// Emit additions from newIdx to m.newIdx
		for newIdx < m.newIdx {
			result = append(result, diffLine{
				text:    newLines[newIdx],
				change:  '+',
				newLine: newIdx + 1,
			})
			newIdx++
		}
		// Emit the matching line
		result = append(result, diffLine{
			text:    oldLines[oldIdx],
			change:  ' ',
			oldLine: oldIdx + 1,
			newLine: newIdx + 1,
		})
		oldIdx++
		newIdx++
	}

	// Emit remaining deletions
	for oldIdx < len(oldLines) {
		result = append(result, diffLine{
			text:    oldLines[oldIdx],
			change:  '-',
			oldLine: oldIdx + 1,
		})
		oldIdx++
	}
	// Emit remaining additions
	for newIdx < len(newLines) {
		result = append(result, diffLine{
			text:    newLines[newIdx],
			change:  '+',
			newLine: newIdx + 1,
		})
		newIdx++
	}

	// Check if there are any changes
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
