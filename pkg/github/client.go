package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

const (
	commentIdentifier = "<!-- argo-diff -->"
	maxCommentSize    = 60000
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

// PostComment posts or updates a comment on a pull request
func (c *Client) PostComment(ctx context.Context, prNumber int, body string) error {
	// Add identifier to help find and update the comment
	body = commentIdentifier + "\n\n" + body

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
		if comment.Body != nil && strings.HasPrefix(*comment.Body, commentIdentifier) {
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

// DeleteOldComments deletes old argo-diff comments from a pull request
func (c *Client) DeleteOldComments(ctx context.Context, prNumber int) error {
	comments, _, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, prNumber, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}

	for _, comment := range comments {
		if comment.Body != nil && strings.HasPrefix(*comment.Body, commentIdentifier) {
			_, err = c.client.Issues.DeleteComment(ctx, c.owner, c.repo, *comment.ID)
			if err != nil {
				return fmt.Errorf("delete comment %d: %w", *comment.ID, err)
			}
		}
	}

	return nil
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
