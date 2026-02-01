package matcher

import (
	"path/filepath"
	"strings"

	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
)

// MatchResult contains information about a matched application
type MatchResult struct {
	App          *appv1.Application
	MatchedPaths []string // Which changed files triggered the match
	MatchReason  string   // Why the app was matched (source path, app definition, etc.)
}

// MatchApplications returns applications affected by changed files
func MatchApplications(apps []*appv1.Application, repo string, changedFiles []string) []*appv1.Application {
	results := MatchApplicationsWithDetails(apps, repo, changedFiles)
	matched := make([]*appv1.Application, 0, len(results))
	for _, r := range results {
		matched = append(matched, r.App)
	}
	return matched
}

// MatchApplicationsWithDetails returns applications affected by changed files with match details
func MatchApplicationsWithDetails(apps []*appv1.Application, repo string, changedFiles []string) []*MatchResult {
	var results []*MatchResult
	for _, app := range apps {
		if result := matchApp(app, repo, changedFiles); result != nil {
			results = append(results, result)
		}
	}
	return results
}

// matchApp checks if an application is affected by the changed files and returns match details
func matchApp(app *appv1.Application, repo string, changedFiles []string) *MatchResult {
	result := &MatchResult{
		App:          app,
		MatchedPaths: []string{},
	}

	// Check if any changed file is an application definition file for this app
	for _, file := range changedFiles {
		if isAppDefinitionFile(file, app.Name) {
			result.MatchedPaths = append(result.MatchedPaths, file)
			result.MatchReason = "application definition changed"
		}
	}

	// Check source paths
	if app.Spec.Source != nil {
		if paths := matchesSourceWithPaths(app.Spec.Source, repo, changedFiles); len(paths) > 0 {
			result.MatchedPaths = append(result.MatchedPaths, paths...)
			if result.MatchReason == "" {
				result.MatchReason = "source path match"
			}
		}
	}

	// Check multi-source paths
	for _, source := range app.Spec.Sources {
		if paths := matchesSourceWithPaths(&source, repo, changedFiles); len(paths) > 0 {
			result.MatchedPaths = append(result.MatchedPaths, paths...)
			if result.MatchReason == "" {
				result.MatchReason = "multi-source path match"
			}
		}
	}

	if len(result.MatchedPaths) > 0 {
		// Deduplicate matched paths
		result.MatchedPaths = uniqueStrings(result.MatchedPaths)
		return result
	}

	return nil
}

// isAppDefinitionFile checks if a file is an application definition for the given app name
// Matches patterns like:
// - applications/<app_name>.yaml
// - applications/<app_name>.yml
// - applications/*/<app_name>.yaml
// - apps/<app_name>.yaml
func isAppDefinitionFile(file, appName string) bool {
	file = strings.TrimPrefix(file, "/")
	base := filepath.Base(file)
	dir := filepath.Dir(file)

	// Check if filename matches app name
	expectedNames := []string{
		appName + ".yaml",
		appName + ".yml",
	}

	nameMatches := false
	for _, expected := range expectedNames {
		if base == expected {
			nameMatches = true
			break
		}
	}

	if !nameMatches {
		return false
	}

	// Check if it's in an applications directory
	appDirs := []string{"applications", "apps", "argocd", "argo-cd"}
	for _, appDir := range appDirs {
		// Direct: applications/<app>.yaml
		if dir == appDir {
			return true
		}
		// Nested: applications/group/<app>.yaml
		if strings.HasPrefix(dir, appDir+"/") {
			return true
		}
	}

	return false
}

// matchesSourceWithPaths checks if a source matches the repository and returns matched file paths
func matchesSourceWithPaths(source *appv1.ApplicationSource, repo string, changedFiles []string) []string {
	if source == nil {
		return nil
	}

	sourceRepo := normalizeRepoURL(source.RepoURL)
	targetRepo := normalizeRepoURL(repo)

	if sourceRepo != targetRepo {
		return nil
	}

	// If no path specified, any change in the repo matches
	if source.Path == "" {
		return changedFiles
	}

	sourcePath := strings.TrimPrefix(source.Path, "/")
	var matched []string

	for _, file := range changedFiles {
		file = strings.TrimPrefix(file, "/")

		// Direct path match
		if strings.HasPrefix(file, sourcePath+"/") || file == sourcePath {
			matched = append(matched, file)
			continue
		}

		// Glob pattern match
		if strings.Contains(sourcePath, "*") {
			if match, _ := filepath.Match(sourcePath, file); match {
				matched = append(matched, file)
				continue
			}
			fileDir := filepath.Dir(file)
			if match, _ := filepath.Match(sourcePath, fileDir); match {
				matched = append(matched, file)
				continue
			}
		}
	}

	return matched
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

// uniqueStrings returns a deduplicated slice of strings
func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
