package worker

// Job represents a diff generation job
type Job struct {
	// GitHub information
	Repository   string
	PRNumber     int
	BaseRef      string
	HeadRef      string
	ChangedFiles []string
	GitHubToken  string
	WorkflowName string

	// ArgoCD information
	ArgocdServer    string
	ArgocdToken     string
	ArgocdPlainText bool
}
