package github

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/go-github/v88/github"
	"github.com/tamcore/argo-diff/pkg/metrics"
)

const (
	commentIdentifierPrefix = "<!-- argocd-diff-workflow:"
	maxCommentSize          = 60000
)

// Client wraps GitHub API client
type Client struct {
	client *github.Client
	owner  string
	repo   string
}

// NewClient creates a new GitHub API client
func NewClient(ctx context.Context, token, owner, repo string) (*Client, error) {
	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}

	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
	}, nil
}

// workflowIdentifier returns the comment identifier for a specific workflow
func workflowIdentifier(workflowName string) string {
	return fmt.Sprintf("%s %s -->", commentIdentifierPrefix, workflowName)
}

// isWorkflowComment checks if a comment body belongs to a specific workflow
func isWorkflowComment(body, workflowName string) bool {
	return strings.Contains(body, workflowIdentifier(workflowName))
}

// PostComment posts or updates comments on a pull request
// Handles multi-part comments if the content exceeds GitHub's limit
// If collapseThreshold > 0 and the number of parts exceeds it, all <details open> tags are collapsed
func (c *Client) PostComment(ctx context.Context, prNumber int, body, workflowName string, collapseThreshold int) error {
	// Delete old comments first
	if err := c.DeleteOldComments(ctx, prNumber, workflowName); err != nil {
		return fmt.Errorf("delete old comments: %w", err)
	}

	// Split into parts if needed
	parts := splitComment(body, workflowName)

	// If we exceed the collapse threshold, collapse all <details open> to <details>
	if collapseThreshold > 0 && len(parts) > collapseThreshold {
		slog.Info("Collapsing all details tags due to threshold exceeded",
			"parts", len(parts),
			"threshold", collapseThreshold,
		)
		for i := range parts {
			parts[i] = strings.ReplaceAll(parts[i], "<details open>", "<details>")
		}
	}

	for i, part := range parts {
		var partBody string
		if len(parts) > 1 {
			partBody = fmt.Sprintf("## ArgoCD Diff Preview (part %d of %d)\n\n%s\n\n%s",
				i+1, len(parts), workflowIdentifier(workflowName), part)
		} else {
			partBody = fmt.Sprintf("%s\n\n%s", workflowIdentifier(workflowName), part)
		}

		_, _, err := c.client.Issues.CreateComment(ctx, c.owner, c.repo, prNumber, &github.IssueComment{
			Body: &partBody,
		})
		metrics.RecordGithubCall("create_comment", err)
		if err != nil {
			return fmt.Errorf("create comment part %d: %w", i+1, err)
		}
	}

	return nil
}

// DeleteOldComments deletes old argo-diff comments from a pull request for a specific workflow
func (c *Client) DeleteOldComments(ctx context.Context, prNumber int, workflowName string) error {
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, prNumber, opts)
		metrics.RecordGithubCall("list_comments", err)
		if err != nil {
			return fmt.Errorf("list comments: %w", err)
		}

		for _, comment := range comments {
			if comment.Body == nil || !isWorkflowComment(*comment.Body, workflowName) {
				continue
			}

			slog.Debug("Deleting old workflow comment", "id", *comment.ID, "pr", prNumber)
			_, err = c.client.Issues.DeleteComment(ctx, c.owner, c.repo, *comment.ID)
			metrics.RecordGithubCall("delete_comment", err)
			if err != nil {
				return fmt.Errorf("delete comment %d: %w", *comment.ID, err)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return nil
}

// splitComment splits a large comment into multiple parts at application boundaries
func splitComment(body, workflowName string) []string {
	effectiveMax := maxCommentSize - 500 // Leave room for header

	// If it fits in one comment, return as-is
	if len(body) <= effectiveMax {
		return []string{body}
	}

	// Try to split at </details> boundaries (end of each resource diff)
	// This is safer than splitting on --- which appears in diff headers
	detailsPattern := regexp.MustCompile(`(?m)</details>\n*`)
	sections := detailsPattern.Split(body, -1)

	var parts []string
	var currentPart strings.Builder

	for i, section := range sections {
		// Add back the </details> tag except for the last section
		var fullSection string
		if i < len(sections)-1 {
			fullSection = section + "</details>\n\n"
		} else {
			fullSection = section
		}

		// Skip empty sections
		if strings.TrimSpace(fullSection) == "" {
			continue
		}

		// If this single section is too large, truncate it
		if len(fullSection) > effectiveMax {
			// First, save any accumulated content
			if currentPart.Len() > 0 {
				parts = append(parts, currentPart.String())
				currentPart.Reset()
			}
			// Truncate the oversized section
			truncated := truncateSection(fullSection, effectiveMax)
			parts = append(parts, truncated)
			continue
		}

		// Check if adding this section would exceed the limit
		if currentPart.Len()+len(fullSection) > effectiveMax && currentPart.Len() > 0 {
			parts = append(parts, currentPart.String())
			currentPart.Reset()
		}

		currentPart.WriteString(fullSection)
	}

	// Don't forget the last part
	if currentPart.Len() > 0 {
		parts = append(parts, currentPart.String())
	}

	// If we couldn't split nicely, just truncate
	if len(parts) == 0 {
		parts = []string{truncateSection(body, effectiveMax)}
	}

	return parts
}

// truncateSection truncates an oversized section while preserving markdown structure
func truncateSection(s string, maxSize int) string {
	if len(s) <= maxSize {
		return s
	}

	// Reserve space for truncation message and closing tags
	truncationMsg := "\n\n... (diff truncated - too large to display)\n```\n</details>\n"
	targetSize := maxSize - len(truncationMsg) - 100

	// Find a good break point (newline)
	breakPoint := targetSize
	for i := targetSize; i > targetSize-500 && i > 0; i-- {
		if s[i] == '\n' {
			breakPoint = i
			break
		}
	}

	truncated := s[:breakPoint]

	// Check if we're inside a code block (odd number of ```)
	codeBlocks := strings.Count(truncated, "```")
	inCodeBlock := codeBlocks%2 == 1

	// Check if we're inside a details block
	detailsOpens := strings.Count(truncated, "<details")
	detailsCloses := strings.Count(truncated, "</details>")
	inDetails := detailsOpens > detailsCloses

	// Add appropriate closing tags
	suffix := "\n\n... (diff truncated - too large to display)\n"
	if inCodeBlock {
		suffix += "```\n"
	}
	if inDetails {
		suffix += "</details>\n"
	}

	return truncated + suffix
}
