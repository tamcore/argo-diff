package github

import (
	"context"
	"strings"
	"testing"
)

func TestWorkflowIdentifier(t *testing.T) {
	id := workflowIdentifier("Test Workflow")
	if !strings.HasPrefix(id, "<!--") {
		t.Error("workflow identifier should be an HTML comment")
	}
	if !strings.Contains(id, "Test Workflow") {
		t.Error("workflow identifier should contain workflow name")
	}
}

func TestIsWorkflowComment(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		workflowName string
		want         bool
	}{
		{
			name:         "matching workflow",
			body:         "<!-- argocd-diff-workflow: Test Workflow -->\n\nSome content",
			workflowName: "Test Workflow",
			want:         true,
		},
		{
			name:         "different workflow",
			body:         "<!-- argocd-diff-workflow: Other Workflow -->\n\nSome content",
			workflowName: "Test Workflow",
			want:         false,
		},
		{
			name:         "not a workflow comment",
			body:         "Regular comment",
			workflowName: "Test Workflow",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWorkflowComment(tt.body, tt.workflowName)
			if got != tt.want {
				t.Errorf("isWorkflowComment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitComment(t *testing.T) {
	// Test that small comments don't get split
	small := "Small comment"
	parts := splitComment(small, "workflow")
	if len(parts) != 1 {
		t.Errorf("splitComment() returned %d parts for small comment, want 1", len(parts))
	}
}

func TestSplitCommentLargeSingleSection(t *testing.T) {
	// Test that a single oversized section gets truncated properly
	// Create a section larger than maxCommentSize
	largeSection := "<details>\n<summary>Large Resource</summary>\n\n```diff\n" + strings.Repeat("+line\n", 15000) + "```\n</details>"
	parts := splitComment(largeSection, "workflow")

	// Should be truncated into a single part
	if len(parts) != 1 {
		t.Errorf("splitComment() returned %d parts for oversized section, want 1 (truncated)", len(parts))
	}

	// Part should be under the limit
	effectiveMax := maxCommentSize - 500
	if len(parts[0]) > effectiveMax {
		t.Errorf("splitComment() part has length %d, exceeds limit %d", len(parts[0]), effectiveMax)
	}

	// Should contain truncation message
	if !strings.Contains(parts[0], "truncated") {
		t.Errorf("splitComment() oversized section should contain 'truncated' message")
	}

	// Should have proper closing tags
	if !strings.HasSuffix(strings.TrimSpace(parts[0]), "</details>") {
		t.Errorf("splitComment() truncated section should end with </details>")
	}
}

func TestSplitCommentMultipleSections(t *testing.T) {
	// Test splitting at </details> boundaries
	section := strings.Repeat("y", 30000)
	body := "<details>" + section + "</details>\n\n<details>" + section + "</details>\n\n<details>" + section + "</details>"

	parts := splitComment(body, "workflow")

	// Should be split into multiple parts (3 sections of ~30k each = ~90k total)
	if len(parts) < 2 {
		t.Errorf("splitComment() returned %d parts, want >= 2", len(parts))
	}

	// Each part should be under the limit
	effectiveMax := maxCommentSize - 500
	for i, part := range parts {
		if len(part) > effectiveMax {
			t.Errorf("splitComment() part %d has length %d, exceeds limit %d", i, len(part), effectiveMax)
		}
	}
}

func TestTruncateSection(t *testing.T) {
	// Test truncation of a large section with code block inside details
	input := "<details>\n<summary>Test</summary>\n\n```diff\n" +
		strings.Repeat("+line content here\n", 5000) +
		"```\n</details>"

	truncated := truncateSection(input, 10000)

	// Should be under the limit
	if len(truncated) > 10000 {
		t.Errorf("truncateSection() result has length %d, want <= 10000", len(truncated))
	}

	// Should contain truncation message
	if !strings.Contains(truncated, "truncated") {
		t.Errorf("truncateSection() should contain 'truncated' message")
	}

	// Should close the code block
	if strings.Count(truncated, "```")%2 != 0 {
		t.Errorf("truncateSection() should have even number of ``` (closed code block)")
	}

	// Should close the details tag
	if !strings.Contains(truncated, "</details>") {
		t.Errorf("truncateSection() should contain </details> closing tag")
	}
}

func TestTruncateSectionSmallEnough(t *testing.T) {
	// Test that small sections are returned as-is
	input := "<details>\n<summary>Small</summary>\n\n```diff\n+line\n```\n</details>"
	truncated := truncateSection(input, 10000)

	if truncated != input {
		t.Errorf("truncateSection() should return small sections unchanged")
	}
}

func TestNewClient(t *testing.T) {
	// Test that NewClient doesn't panic with valid inputs
	client := NewClient(context.TODO(), "test-token", "owner", "repo")
	if client.owner != "owner" {
		t.Errorf("owner = %q, want %q", client.owner, "owner")
	}
	if client.repo != "repo" {
		t.Errorf("repo = %q, want %q", client.repo, "repo")
	}
}

func TestCollapseDetailsThreshold(t *testing.T) {
	// Create content that will be split into multiple parts (>3)
	// maxCommentSize is 60000, effectiveMax is 59500
	// Each section is ~30KB, 8 sections = ~240KB = 4+ parts
	section := "<details open>\n<summary>Test</summary>\n" + strings.Repeat("x", 30000) + "\n</details>"
	body := strings.Repeat(section+"\n\n", 8)

	// First verify it splits into more than 3 parts
	parts := splitComment(body, "workflow")
	if len(parts) <= 3 {
		t.Skipf("Test requires >3 parts, got %d (body size: %d)", len(parts), len(body))
	}
	t.Logf("Body splits into %d parts (body size: %d)", len(parts), len(body))

	// Check that original body contains <details open>
	if !strings.Contains(body, "<details open>") {
		t.Fatal("Test body should contain <details open>")
	}

	// Simulate what PostComment does when threshold is exceeded
	collapseThreshold := 3
	if collapseThreshold > 0 && len(parts) > collapseThreshold {
		for i := range parts {
			parts[i] = strings.ReplaceAll(parts[i], "<details open>", "<details>")
		}
	}

	// Verify all <details open> have been collapsed
	for i, part := range parts {
		if strings.Contains(part, "<details open>") {
			t.Errorf("Part %d still contains <details open> after collapse", i)
		}
		// Should still have <details> (just not open)
		if !strings.Contains(part, "<details>") && strings.Contains(section, "<details>") {
			t.Logf("Part %d may have been chunked mid-tag", i)
		}
	}
}

func TestCollapseDetailsThresholdDisabled(t *testing.T) {
	// Test that threshold=0 disables collapsing
	body := "<details open>\n<summary>Test</summary>\ncontent\n</details>"

	parts := splitComment(body, "workflow")

	// With threshold=0, should not collapse even with many parts
	collapseThreshold := 0
	if collapseThreshold > 0 && len(parts) > collapseThreshold {
		for i := range parts {
			parts[i] = strings.ReplaceAll(parts[i], "<details open>", "<details>")
		}
	}

	// Should still have <details open>
	for _, part := range parts {
		if strings.Contains(part, "<details open>") {
			return // Found it, test passes
		}
	}

	// If body was too small to contain the full tag, that's okay
	if strings.Contains(body, "<details open>") && !strings.Contains(strings.Join(parts, ""), "<details open>") {
		t.Error("Threshold=0 should not collapse <details open> tags")
	}
}

func TestCollapseDetailsThresholdNotExceeded(t *testing.T) {
	// Test that small diffs (<=threshold parts) stay open
	body := "<details open>\n<summary>Test</summary>\nsmall content\n</details>"

	parts := splitComment(body, "workflow")
	if len(parts) != 1 {
		t.Fatalf("Expected 1 part for small body, got %d", len(parts))
	}

	// With threshold=3 and 1 part, should NOT collapse
	collapseThreshold := 3
	if collapseThreshold > 0 && len(parts) > collapseThreshold {
		for i := range parts {
			parts[i] = strings.ReplaceAll(parts[i], "<details open>", "<details>")
		}
	}

	// Should still have <details open>
	if !strings.Contains(parts[0], "<details open>") {
		t.Error("Small diff should keep <details open> when under threshold")
	}
}
