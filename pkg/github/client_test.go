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
