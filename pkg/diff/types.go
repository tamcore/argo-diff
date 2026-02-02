package diff

import (
	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// AppInfo contains metadata about an ArgoCD application for diff generation
type AppInfo struct {
	Name      string
	Namespace string
	Server    string // ArgoCD server URL for generating links
	Status    string // Synced, OutOfSync, Unknown
	Health    string // Healthy, Progressing, Degraded, Suspended, Missing, Unknown
}

// NewAppInfo creates AppInfo from an ArgoCD application
func NewAppInfo(app *appv1.Application, serverURL string) *AppInfo {
	info := &AppInfo{
		Name:      app.Name,
		Namespace: app.Namespace,
		Server:    serverURL,
		Status:    "Unknown",
		Health:    "Unknown",
	}

	if app.Status.Sync.Status != "" {
		info.Status = string(app.Status.Sync.Status)
	}

	if app.Status.Health.Status != "" {
		info.Health = string(app.Status.Health.Status)
	}

	return info
}

// StatusEmoji returns the emoji for sync status
func (a *AppInfo) StatusEmoji() string {
	switch a.Status {
	case "Synced":
		return "✅"
	case "OutOfSync":
		return "❌"
	default:
		return "❓"
	}
}

// HealthEmoji returns the emoji for health status
func (a *AppInfo) HealthEmoji() string {
	switch a.Health {
	case "Healthy":
		return "💚"
	case "Progressing":
		return "🔄"
	case "Degraded":
		return "💔"
	case "Suspended":
		return "⏸️"
	case "Missing":
		return "❓"
	default:
		return "❓"
	}
}

// ArgoURL returns the URL to view the application in ArgoCD UI
func (a *AppInfo) ArgoURL() string {
	if a.Server == "" {
		return ""
	}
	// Remove trailing slash if present
	server := a.Server
	if len(server) > 0 && server[len(server)-1] == '/' {
		server = server[:len(server)-1]
	}
	// ArgoCD UI URL format
	return server + "/applications/" + a.Namespace + "/" + a.Name
}

// DiffResult contains the result of diffing an application
type DiffResult struct {
	AppInfo      *AppInfo
	Diffs        []string // Individual resource diffs
	HasChanges   bool
	ErrorMessage string
	// Resource change counts
	ResourcesAdded    int
	ResourcesModified int
	ResourcesDeleted  int
}

// DiffReport contains the complete diff report for all applications
type DiffReport struct {
	WorkflowName  string
	Timestamp     string
	TotalApps     int
	AppsWithDiffs int
	Results       []*DiffResult
}
