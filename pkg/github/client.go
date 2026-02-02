package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/go-github/v68/github"
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
	token  string
}

// NewClient creates a new GitHub API client
func NewClient(ctx context.Context, token, owner, repo string) *Client {
	// Use go-github's built-in auth token method
	client := github.NewClient(nil).WithAuthToken(token)

	slog.Info("Created GitHub client",
		"owner", owner,
		"repo", repo,
		"token_length", len(token),
	)

	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
		token:  token,
	}
}

// makeDirectRequest makes a direct HTTP request to the GitHub API for debugging
func (c *Client) makeDirectRequest(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "argo-diff")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
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
		metrics.RecordGithubCall("create_comment", err)
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
	// Debug: verify token works with direct HTTP call before using go-github
	testURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments?per_page=1",
		c.owner, c.repo, prNumber)
	statusCode, err := c.makeDirectRequest(ctx, testURL)
	if err != nil {
		slog.Error("Direct HTTP test failed in DeleteOldComments", "error", err)
	} else {
		slog.Info("Direct HTTP test in DeleteOldComments", "status_code", statusCode, "url", testURL)
	}

	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	identifier := workflowIdentifier(workflowName)
	slog.Info("DeleteOldComments: looking for comments",
		"pr", prNumber,
		"workflow", workflowName,
		"identifier", identifier,
	)

	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, prNumber, opts)
		metrics.RecordGithubCall("list_comments", err)
		if err != nil {
			slog.Error("go-github ListComments failed",
				"error", err,
				"owner", c.owner,
				"repo", c.repo,
				"pr", prNumber,
			)
			return fmt.Errorf("list comments: %w", err)
		}

		slog.Info("DeleteOldComments: found comments", "count", len(comments), "page", opts.Page)

		for _, comment := range comments {
			if comment.Body == nil {
				continue
			}
			// Log first 100 chars of each comment for debugging
			preview := *comment.Body
			if len(preview) > 100 {
				preview = preview[:100]
			}
			isMatch := isWorkflowComment(*comment.Body, workflowName)
			slog.Info("DeleteOldComments: checking comment",
				"id", *comment.ID,
				"matches", isMatch,
				"preview", preview,
			)

			if isMatch {
				slog.Info("DeleteOldComments: deleting comment", "id", *comment.ID)
				_, err = c.client.Issues.DeleteComment(ctx, c.owner, c.repo, *comment.ID)
				metrics.RecordGithubCall("delete_comment", err)
				if err != nil {
					slog.Error("DeleteOldComments: failed to delete", "id", *comment.ID, "error", err)
					return fmt.Errorf("delete comment %d: %w", *comment.ID, err)
				}
				slog.Info("DeleteOldComments: successfully deleted comment", "id", *comment.ID)
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
