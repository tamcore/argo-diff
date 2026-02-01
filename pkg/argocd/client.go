package argocd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/argoproj/argo-cd/v3/pkg/apiclient"
	"github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/tamcore/argo-diff/pkg/metrics"
)

// Client wraps the ArgoCD API client
type Client struct {
	appClient application.ApplicationServiceClient
	conn      io.Closer
	server    string
}

// NewClient creates a new ArgoCD client
func NewClient(ctx context.Context, server, token string, insecureTLS bool) (*Client, error) {
	opts := apiclient.ClientOptions{
		ServerAddr: server,
		AuthToken:  token,
		Insecure:   insecureTLS,
		GRPCWeb:    true,
	}

	clientset := apiclient.NewClientOrDie(&opts)
	conn, appClient, err := clientset.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create ArgoCD application client: %w", err)
	}

	return &Client{
		appClient: appClient,
		conn:      conn,
		server:    server,
	}, nil
}

// Server returns the ArgoCD server URL
func (c *Client) Server() string {
	return c.server
}

// Close closes the connection to ArgoCD
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ListApplications lists all applications in ArgoCD
func (c *Client) ListApplications(ctx context.Context) ([]*appv1.Application, error) {
	var apps []*appv1.Application
	err := retry(ctx, 3, func() error {
		query := &application.ApplicationQuery{}
		appList, err := c.appClient.List(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to list applications: %w", err)
		}
		apps = nil // Reset on retry
		for i := range appList.Items {
			apps = append(apps, &appList.Items[i])
		}
		return nil
	})
	metrics.RecordArgocdCall("list", err)
	return apps, err
}

// GetManifests fetches the manifests for a specific application and revision
func (c *Client) GetManifests(ctx context.Context, appName, revision string) ([]string, error) {
	var manifests []string
	err := retry(ctx, 3, func() error {
		query := &application.ApplicationManifestQuery{
			Name:     &appName,
			Revision: &revision,
		}
		manifestResponse, err := c.appClient.GetManifests(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to get manifests for app %s: %w", appName, err)
		}
		manifests = manifestResponse.Manifests
		return nil
	})
	metrics.RecordArgocdCall("manifests", err)
	return manifests, err
}

// MultiSourceRevision represents a revision for a specific source in a multi-source app
type MultiSourceRevision struct {
	Revision       string
	SourcePosition int // 1-based position
}

// GetMultiSourceManifests fetches manifests for a multi-source application with specific revisions
// Each source can have its own revision specified by position
func (c *Client) GetMultiSourceManifests(ctx context.Context, appName string, revisions []MultiSourceRevision) ([]string, error) {
	var manifests []string
	err := retry(ctx, 3, func() error {
		// Build the revisions and source positions arrays
		revisionList := make([]string, 0, len(revisions))
		sourcePositions := make([]int64, 0, len(revisions))

		for _, r := range revisions {
			revisionList = append(revisionList, r.Revision)
			sourcePositions = append(sourcePositions, int64(r.SourcePosition))
		}

		query := &application.ApplicationManifestQuery{
			Name:            &appName,
			Revisions:       revisionList,
			SourcePositions: sourcePositions,
		}

		manifestResponse, err := c.appClient.GetManifests(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to get multi-source manifests for app %s: %w", appName, err)
		}
		manifests = manifestResponse.Manifests
		return nil
	})
	metrics.RecordArgocdCall("manifests_multi", err)
	return manifests, err
}

// IsMultiSource returns true if the application has multiple sources
func IsMultiSource(app *appv1.Application) bool {
	return len(app.Spec.Sources) > 0
}

// GetSourceCount returns the number of sources for an application
func GetSourceCount(app *appv1.Application) int {
	if len(app.Spec.Sources) > 0 {
		return len(app.Spec.Sources)
	}
	if app.Spec.Source != nil {
		return 1
	}
	return 0
}

// retry executes a function with exponential backoff
func retry(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}

		if i < attempts-1 {
			delay := time.Duration(5*(i+1)) * time.Second
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", attempts, err)
}
