package matcher

import (
	"testing"

	appv1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMatchApplications(t *testing.T) {
	tests := []struct {
		name         string
		apps         []*appv1.Application
		repo         string
		changedFiles []string
		wantCount    int
	}{
		{
			name: "exact path match",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "app1"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://github.com/user/repo",
							Path:    "app1",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"app1/deployment.yaml"},
			wantCount:    1,
		},
		{
			name: "no match different repo",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "app1"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://github.com/user/other",
							Path:    "app1",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"app1/deployment.yaml"},
			wantCount:    0,
		},
		{
			name: "case insensitive repo match",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "app1"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://GitHub.com/User/Repo.git",
							Path:    "app1",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"app1/deployment.yaml"},
			wantCount:    1,
		},
		{
			name: "wildcard path match",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "app1"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://github.com/user/repo",
							Path:    "applications/*.yaml",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"applications/app1.yaml"},
			wantCount:    1,
		},
		{
			name: "multi-source application",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "app1"},
					Spec: appv1.ApplicationSpec{
						Sources: []appv1.ApplicationSource{
							{
								RepoURL: "https://github.com/user/repo",
								Path:    "app1",
							},
							{
								RepoURL: "https://github.com/user/repo",
								Path:    "app2",
							},
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"app2/deployment.yaml"},
			wantCount:    1,
		},
		{
			name: "app definition file match",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://github.com/user/repo",
							Path:    "charts/myapp",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"applications/myapp.yaml"},
			wantCount:    1,
		},
		{
			name: "nested app definition file match",
			apps: []*appv1.Application{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp"},
					Spec: appv1.ApplicationSpec{
						Source: &appv1.ApplicationSource{
							RepoURL: "https://github.com/user/repo",
							Path:    "charts/myapp",
						},
					},
				},
			},
			repo:         "https://github.com/user/repo",
			changedFiles: []string{"applications/prod/myapp.yaml"},
			wantCount:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchApplications(tt.apps, tt.repo, tt.changedFiles)
			if len(got) != tt.wantCount {
				t.Errorf("MatchApplications() returned %d apps, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestIsAppDefinitionFile(t *testing.T) {
	tests := []struct {
		file    string
		appName string
		want    bool
	}{
		{"applications/myapp.yaml", "myapp", true},
		{"applications/myapp.yml", "myapp", true},
		{"applications/prod/myapp.yaml", "myapp", true},
		{"apps/myapp.yaml", "myapp", true},
		{"argocd/myapp.yaml", "myapp", true},
		{"applications/other.yaml", "myapp", false},
		{"charts/myapp/values.yaml", "myapp", false},
		{"myapp.yaml", "myapp", false}, // not in app directory
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got := isAppDefinitionFile(tt.file, tt.appName)
			if got != tt.want {
				t.Errorf("isAppDefinitionFile(%q, %q) = %v, want %v", tt.file, tt.appName, got, tt.want)
			}
		})
	}
}

func TestMatchApplicationsWithDetails(t *testing.T) {
	apps := []*appv1.Application{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "app1"},
			Spec: appv1.ApplicationSpec{
				Source: &appv1.ApplicationSource{
					RepoURL: "https://github.com/user/repo",
					Path:    "app1",
				},
			},
		},
	}

	results := MatchApplicationsWithDetails(apps, "https://github.com/user/repo", []string{"app1/deployment.yaml"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].MatchReason == "" {
		t.Error("MatchReason should not be empty")
	}
	if len(results[0].MatchedPaths) == 0 {
		t.Error("MatchedPaths should not be empty")
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "https://github.com/user/repo.git",
			want:  "github.com/user/repo",
		},
		{
			input: "git@github.com:user/repo.git",
			want:  "github.com/user/repo",
		},
		{
			input: "ssh://git@github.com/user/repo",
			want:  "github.com/user/repo",
		},
		{
			input: "https://GitHub.com/User/Repo/",
			want:  "github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRepoURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRepoURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
