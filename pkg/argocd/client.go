package argocd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/argoproj/argo-cd/v3/pkg/apiclient"
	"github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// Client wraps the ArgoCD API client
type Client struct {
	appClient application.ApplicationServiceClient
	conn      io.Closer
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
	}, nil
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
		for i := range appList.Items {
			apps = append(apps, &appList.Items[i])
		}
		return nil
	})
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
	return manifests, err
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
