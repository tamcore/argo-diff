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
	// Test that a single oversized section gets chunked properly
	// Create a section larger than maxCommentSize
	largeSection := "<details>\n" + strings.Repeat("x", 70000) + "\n</details>"
	parts := splitComment(largeSection, "workflow")

	// Should be split into multiple parts
	if len(parts) < 2 {
		t.Errorf("splitComment() returned %d parts for oversized section, want >= 2", len(parts))
	}

	// Each part should be under the limit
	effectiveMax := maxCommentSize - 500
	for i, part := range parts {
		if len(part) > effectiveMax {
			t.Errorf("splitComment() part %d has length %d, exceeds limit %d", i, len(part), effectiveMax)
		}
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

func TestChunkString(t *testing.T) {
	input := strings.Repeat("a", 100)
	chunks := chunkString(input, 30)
	if len(chunks) != 4 {
		t.Errorf("chunkString() returned %d chunks, want 4", len(chunks))
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
