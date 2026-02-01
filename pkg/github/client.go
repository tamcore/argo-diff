package github

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
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
func NewClient(ctx context.Context, token, owner, repo string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return &Client{
		client: github.NewClient(tc),
		owner:  owner,
		repo:   repo,
	}
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
func (c *Client) PostComment(ctx context.Context, prNumber int, body, workflowName string) error {
	// Delete old comments first
	if err := c.DeleteOldComments(ctx, prNumber, workflowName); err != nil {
		return fmt.Errorf("delete old comments: %w", err)
	}

	// Split into parts if needed
	parts := splitComment(body, workflowName)

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
		if err != nil {
			return fmt.Errorf("create comment part %d: %w", i+1, err)
		}
	}

	return nil
}

// PostCommentLegacy posts or updates a comment on a pull request (legacy, single comment)
func (c *Client) PostCommentLegacy(ctx context.Context, prNumber int, body string) error {
	identifier := "<!-- argo-diff -->"
	body = identifier + "\n\n" + body

	// Truncate if too large
	if len(body) > maxCommentSize {
		body = body[:maxCommentSize-100] + "\n\n... (comment truncated due to size)"
	}

	// Find existing comment
	comments, _, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, prNumber, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}

	var existingComment *github.IssueComment
	for _, comment := range comments {
		if comment.Body != nil && strings.HasPrefix(*comment.Body, identifier) {
			existingComment = comment
			break
		}
	}

	// Update or create comment
	if existingComment != nil {
		_, _, err = c.client.Issues.EditComment(ctx, c.owner, c.repo, *existingComment.ID, &github.IssueComment{
			Body: &body,
		})
		if err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
	} else {
		_, _, err = c.client.Issues.CreateComment(ctx, c.owner, c.repo, prNumber, &github.IssueComment{
			Body: &body,
		})
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
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
		if err != nil {
			return fmt.Errorf("list comments: %w", err)
		}

		for _, comment := range comments {
			if comment.Body != nil && isWorkflowComment(*comment.Body, workflowName) {
				_, err = c.client.Issues.DeleteComment(ctx, c.owner, c.repo, *comment.ID)
				if err != nil {
					return fmt.Errorf("delete comment %d: %w", *comment.ID, err)
				}
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
	// If it fits in one comment, return as-is
	if len(body) <= maxCommentSize-500 { // Leave room for header
		return []string{body}
	}

	// Try to split at application boundaries (### headers or --- separators)
	appPattern := regexp.MustCompile(`(?m)^(---|### )`)
	sections := appPattern.Split(body, -1)
	matches := appPattern.FindAllString(body, -1)

	var parts []string
	var currentPart strings.Builder

	for i, section := range sections {
		// Reconstruct with the delimiter
		var fullSection string
		if i > 0 && i-1 < len(matches) {
			fullSection = matches[i-1] + section
		} else {
			fullSection = section
		}

		// Check if adding this section would exceed the limit
		if currentPart.Len()+len(fullSection) > maxCommentSize-500 && currentPart.Len() > 0 {
			parts = append(parts, currentPart.String())
			currentPart.Reset()
		}

		currentPart.WriteString(fullSection)
	}

	// Don't forget the last part
	if currentPart.Len() > 0 {
		parts = append(parts, currentPart.String())
	}

	// If we couldn't split nicely, just chunk it
	if len(parts) == 0 {
		parts = chunkString(body, maxCommentSize-500)
	}

	return parts
}

// chunkString splits a string into chunks of max size
func chunkString(s string, chunkSize int) []string {
	var chunks []string
	for len(s) > 0 {
		if len(s) <= chunkSize {
			chunks = append(chunks, s)
			break
		}
		// Try to break at a newline
		breakPoint := chunkSize
		for i := chunkSize; i > chunkSize-100 && i > 0; i-- {
			if s[i] == '\n' {
				breakPoint = i + 1
				break
			}
		}
		chunks = append(chunks, s[:breakPoint])
		s = s[breakPoint:]
	}
	return chunks
}

// GetPullRequest retrieves pull request details
func (c *Client) GetPullRequest(ctx context.Context, prNumber int) (*github.PullRequest, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, c.owner, c.repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	return pr, nil
}

// GetChangedFiles retrieves the list of changed files in a pull request
func (c *Client) GetChangedFiles(ctx context.Context, prNumber int) ([]string, error) {
	var allFiles []string
	opts := &github.ListOptions{PerPage: 100}

	for {
		files, resp, err := c.client.PullRequests.ListFiles(ctx, c.owner, c.repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}

		for _, file := range files {
			if file.Filename != nil {
				allFiles = append(allFiles, *file.Filename)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}
