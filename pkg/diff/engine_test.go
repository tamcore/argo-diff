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

	diff, err := GenerateDiff(baseManifests, headManifests, "test-app")
	if err != nil {
		t.Fatalf("GenerateDiff() error = %v", err)
	}

	if !strings.Contains(diff, "test-app") {
		t.Error("diff should contain app name")
	}
	if !strings.Contains(diff, "Modified") {
		t.Error("diff should indicate modification")
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
