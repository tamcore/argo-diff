package diff

import (
	"strings"
	"testing"
)

func TestGenerateDiff(t *testing.T) {
	baseManifests := []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
spec:
  replicas: 2
`}

	headManifests := []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
spec:
  replicas: 3
`}

	appInfo := &AppInfo{
		Name:      "test-app",
		Namespace: "argocd",
		Server:    "https://argocd.example.com",
		Status:    "Synced",
		Health:    "Healthy",
	}

	result, err := GenerateDiff(baseManifests, headManifests, appInfo)
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if !result.HasChanges {
		t.Error("result should indicate changes")
	}
	if len(result.Diffs) == 0 {
		t.Error("result should contain diffs")
	}
}

func TestGenerateDiffLegacy(t *testing.T) {
	baseManifests := []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
spec:
  replicas: 2
`}

	headManifests := []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
spec:
  replicas: 3
`}

	diff, err := GenerateDiffLegacy(baseManifests, headManifests, "test-app")
	if err != nil {
		t.Fatalf("GenerateDiffLegacy() error = %v", err)
	}

	if !strings.Contains(diff, "test-app") {
		t.Error("diff should contain app name")
	}
	if !strings.Contains(diff, "=====") {
		t.Error("diff should indicate modification with ===== header")
	}
}

func TestFilterHelmHooks(t *testing.T) {
	resources := []*Resource{
		{
			APIVersion: "v1",
			Kind:       "Job",
			Metadata: struct {
				Name        string            `yaml:"name"`
				Namespace   string            `yaml:"namespace,omitempty"`
				Labels      map[string]string `yaml:"labels,omitempty"`
				Annotations map[string]string `yaml:"annotations,omitempty"`
			}{
				Name: "pre-install",
				Annotations: map[string]string{
					helmHookAnnotation: "pre-install",
				},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "Service",
			Metadata: struct {
				Name        string            `yaml:"name"`
				Namespace   string            `yaml:"namespace,omitempty"`
				Labels      map[string]string `yaml:"labels,omitempty"`
				Annotations map[string]string `yaml:"annotations,omitempty"`
			}{
				Name: "app-service",
			},
		},
	}

	filtered := filterHelmHooks(resources)
	if len(filtered) != 1 {
		t.Errorf("filterHelmHooks() returned %d resources, want 1", len(filtered))
	}
	if filtered[0].Metadata.Name != "app-service" {
		t.Errorf("filterHelmHooks() kept wrong resource: %s", filtered[0].Metadata.Name)
	}
}

func TestResourceKey(t *testing.T) {
	tests := []struct {
		name     string
		resource *Resource
		want     string
	}{
		{
			name: "with namespace",
			resource: &Resource{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Metadata: struct {
					Name        string            `yaml:"name"`
					Namespace   string            `yaml:"namespace,omitempty"`
					Labels      map[string]string `yaml:"labels,omitempty"`
					Annotations map[string]string `yaml:"annotations,omitempty"`
				}{
					Name:      "test-app",
					Namespace: "default",
				},
			},
			want: "apps/v1/Deployment/default/test-app",
		},
		{
			name: "without namespace",
			resource: &Resource{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Metadata: struct {
					Name        string            `yaml:"name"`
					Namespace   string            `yaml:"namespace,omitempty"`
					Labels      map[string]string `yaml:"labels,omitempty"`
					Annotations map[string]string `yaml:"annotations,omitempty"`
				}{
					Name: "test-config",
				},
			},
			want: "v1/ConfigMap/test-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.resource.key()
			if got != tt.want {
				t.Errorf("key() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppInfo(t *testing.T) {
	info := &AppInfo{
		Name:      "test-app",
		Namespace: "argocd",
		Server:    "https://argocd.example.com",
		Status:    "Synced",
		Health:    "Healthy",
	}

	if info.StatusEmoji() != "✅" {
		t.Errorf("StatusEmoji() = %q, want ✅", info.StatusEmoji())
	}

	if info.HealthEmoji() != "💚" {
		t.Errorf("HealthEmoji() = %q, want 💚", info.HealthEmoji())
	}

	url := info.ArgoURL()
	if url != "https://argocd.example.com/applications/argocd/test-app" {
		t.Errorf("ArgoURL() = %q, want %q", url, "https://argocd.example.com/applications/argocd/test-app")
	}
}

func TestAppInfoEmojis(t *testing.T) {
	tests := []struct {
		status string
		health string
		wantS  string
		wantH  string
	}{
		{"Synced", "Healthy", "✅", "💚"},
		{"OutOfSync", "Degraded", "❌", "💔"},
		{"Unknown", "Progressing", "❓", "🔄"},
		{"", "Suspended", "❓", "⏸️"},
	}

	for _, tt := range tests {
		info := &AppInfo{Status: tt.status, Health: tt.health}
		if got := info.StatusEmoji(); got != tt.wantS {
			t.Errorf("StatusEmoji(%q) = %q, want %q", tt.status, got, tt.wantS)
		}
		if got := info.HealthEmoji(); got != tt.wantH {
			t.Errorf("HealthEmoji(%q) = %q, want %q", tt.health, got, tt.wantH)
		}
	}
}

func TestFormatReport(t *testing.T) {
	results := []*DiffResult{
		{
			AppInfo:    &AppInfo{Name: "app1", Status: "Synced", Health: "Healthy"},
			HasChanges: true,
			Diffs:      []string{"diff1"},
		},
		{
			AppInfo:    &AppInfo{Name: "app2", Status: "Synced", Health: "Healthy"},
			HasChanges: false,
			Diffs:      []string{},
		},
	}

	report := NewDiffReport("Test Workflow", results)
	if report.TotalApps != 2 {
		t.Errorf("TotalApps = %d, want 2", report.TotalApps)
	}
	if report.AppsWithDiffs != 1 {
		t.Errorf("AppsWithDiffs = %d, want 1", report.AppsWithDiffs)
	}

	formatted := FormatReport(report)
	if !strings.Contains(formatted, "ArgoCD Diff Preview") {
		t.Error("formatted report should contain header")
	}
	if !strings.Contains(formatted, "Test Workflow") {
		t.Error("formatted report should contain workflow name")
	}
	if !strings.Contains(formatted, "1** of **2") {
		t.Error("formatted report should contain summary")
	}
}

func TestDeduplicateResults(t *testing.T) {
	// Create results with identical diffs for app1 and app2
	results := []*DiffResult{
		{
			AppInfo:    &AppInfo{Name: "app1", Status: "Synced", Health: "Healthy"},
			HasChanges: true,
			Diffs:      []string{"identical diff content"},
		},
		{
			AppInfo:    &AppInfo{Name: "app2", Status: "OutOfSync", Health: "Progressing"},
			HasChanges: true,
			Diffs:      []string{"identical diff content"},
		},
		{
			AppInfo:    &AppInfo{Name: "app3", Status: "Synced", Health: "Healthy"},
			HasChanges: true,
			Diffs:      []string{"different diff content"},
		},
	}

	deduplicateResults(results)

	// app1 should be the original (no DuplicateOf)
	if results[0].DuplicateOf != "" {
		t.Errorf("app1 should not be marked as duplicate, got DuplicateOf=%q", results[0].DuplicateOf)
	}

	// app2 should be marked as duplicate of app1
	if results[1].DuplicateOf != "app1" {
		t.Errorf("app2 should be marked as duplicate of app1, got DuplicateOf=%q", results[1].DuplicateOf)
	}

	// app3 has different content, should not be duplicate
	if results[2].DuplicateOf != "" {
		t.Errorf("app3 should not be marked as duplicate, got DuplicateOf=%q", results[2].DuplicateOf)
	}
}

func TestDeduplicateResultsSkipsNoChanges(t *testing.T) {
	// Results without changes should not be deduplicated
	results := []*DiffResult{
		{
			AppInfo:    &AppInfo{Name: "app1"},
			HasChanges: false,
			Diffs:      []string{},
		},
		{
			AppInfo:    &AppInfo{Name: "app2"},
			HasChanges: false,
			Diffs:      []string{},
		},
	}

	deduplicateResults(results)

	if results[0].DuplicateOf != "" {
		t.Errorf("app1 should not be marked as duplicate")
	}
	if results[1].DuplicateOf != "" {
		t.Errorf("app2 should not be marked as duplicate")
	}
}

func TestDeduplicateResultsSkipsErrors(t *testing.T) {
	// Results with errors should not be deduplicated
	results := []*DiffResult{
		{
			AppInfo:      &AppInfo{Name: "app1"},
			HasChanges:   true,
			Diffs:        []string{"diff"},
			ErrorMessage: "error 1",
		},
		{
			AppInfo:      &AppInfo{Name: "app2"},
			HasChanges:   true,
			Diffs:        []string{"diff"},
			ErrorMessage: "error 2",
		},
	}

	deduplicateResults(results)

	if results[0].DuplicateOf != "" {
		t.Errorf("app1 should not be marked as duplicate (has error)")
	}
	if results[1].DuplicateOf != "" {
		t.Errorf("app2 should not be marked as duplicate (has error)")
	}
}

func TestNewDiffReportWithDeduplication(t *testing.T) {
	results := []*DiffResult{
		{
			AppInfo:    &AppInfo{Name: "cert-manager", Status: "Synced", Health: "Healthy"},
			HasChanges: true,
			Diffs:      []string{"same diff"},
		},
		{
			AppInfo:    &AppInfo{Name: "foo-cert-manager", Status: "Synced", Health: "Healthy"},
			HasChanges: true,
			Diffs:      []string{"same diff"},
		},
	}

	// With deduplication enabled (default)
	report := NewDiffReportWithOptions("Test", results, true)
	if !report.DedupeDiffs {
		t.Error("DedupeDiffs should be true")
	}
	if results[1].DuplicateOf != "cert-manager" {
		t.Errorf("foo-cert-manager should be marked as duplicate of cert-manager, got %q", results[1].DuplicateOf)
	}

	// Reset for next test
	results[1].DuplicateOf = ""

	// With deduplication disabled
	report = NewDiffReportWithOptions("Test", results, false)
	if report.DedupeDiffs {
		t.Error("DedupeDiffs should be false")
	}
	if results[1].DuplicateOf != "" {
		t.Errorf("foo-cert-manager should not be marked as duplicate when dedupe is disabled")
	}
}

func TestFormatAppDiffWithDuplicate(t *testing.T) {
	result := &DiffResult{
		AppInfo:     &AppInfo{Name: "app2", Status: "Synced", Health: "Healthy"},
		HasChanges:  true,
		Diffs:       []string{"some diff"},
		DuplicateOf: "app1",
	}

	formatted := FormatAppDiff(result)

	if !strings.Contains(formatted, "Same diff as `app1`") {
		t.Errorf("formatted diff should indicate duplicate, got: %s", formatted)
	}
	if strings.Contains(formatted, "some diff") {
		t.Error("formatted diff should not contain actual diff content for duplicates")
	}
}

func TestArgoURLOptional(t *testing.T) {
	// Without server URL, ArgoURL should return empty string
	info := &AppInfo{
		Name:      "test-app",
		Namespace: "argocd",
		Server:    "",
		Status:    "Synced",
		Health:    "Healthy",
	}

	if url := info.ArgoURL(); url != "" {
		t.Errorf("ArgoURL() should be empty when Server is empty, got %q", url)
	}

	// FormatAppDiff should not include the link
	result := &DiffResult{
		AppInfo:    info,
		HasChanges: true,
		Diffs:      []string{"diff"},
	}
	formatted := FormatAppDiff(result)
	if strings.Contains(formatted, "View in ArgoCD") {
		t.Error("formatted diff should not contain ArgoCD link when URL is empty")
	}
}
