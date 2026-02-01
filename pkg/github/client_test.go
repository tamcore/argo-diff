package github

import (
	"context"
	"strings"
	"testing"
)

func TestCommentIdentifier(t *testing.T) {
	if commentIdentifier == "" {
		t.Error("commentIdentifier should not be empty")
	}
	if !strings.HasPrefix(commentIdentifier, "<!--") {
		t.Error("commentIdentifier should be an HTML comment")
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
