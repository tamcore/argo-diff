package matcher

import (
	"path/filepath"
	"strings"

	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// MatchApplications returns applications affected by changed files
func MatchApplications(apps []*appv1.Application, repo string, changedFiles []string) []*appv1.Application {
	var matched []*appv1.Application
	for _, app := range apps {
		if isAppAffected(app, repo, changedFiles) {
			matched = append(matched, app)
		}
	}
	return matched
}

// isAppAffected checks if an application is affected by the changed files
func isAppAffected(app *appv1.Application, repo string, changedFiles []string) bool {
	if app.Spec.Source != nil {
		if matchesSource(app.Spec.Source, repo, changedFiles) {
			return true
		}
	}
	for _, source := range app.Spec.Sources {
		if matchesSource(&source, repo, changedFiles) {
			return true
		}
	}
	return false
}

// matchesSource checks if a source matches the repository and changed files
func matchesSource(source *appv1.ApplicationSource, repo string, changedFiles []string) bool {
	if source == nil {
		return false
	}
	sourceRepo := normalizeRepoURL(source.RepoURL)
	targetRepo := normalizeRepoURL(repo)
	if sourceRepo != targetRepo {
		return false
	}
	if source.Path == "" {
		return true
	}
	sourcePath := strings.TrimPrefix(source.Path, "/")
	for _, file := range changedFiles {
		file = strings.TrimPrefix(file, "/")
		if strings.HasPrefix(file, sourcePath+"/") || file == sourcePath {
			return true
		}
		if strings.Contains(sourcePath, "*") {
			if match, _ := filepath.Match(sourcePath, file); match {
				return true
			}
			fileDir := filepath.Dir(file)
			if match, _ := filepath.Match(sourcePath, fileDir); match {
				return true
			}
		}
	}
	return false
}

// normalizeRepoURL normalizes a repository URL for comparison
func normalizeRepoURL(url string) string {
	url = strings.ToLower(url)
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")
	// Remove protocol prefixes first
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "ssh://")
	// Handle SSH format (git@github.com:user/repo)
	url = strings.TrimPrefix(url, "git@")
	url = strings.ReplaceAll(url, ":", "/")
	return url
}
