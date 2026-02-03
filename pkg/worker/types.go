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
	ArgocdURL       string // Optional: ArgoCD UI URL for links in comments

	// Options
	DedupeDiffs          bool // Default: true - deduplicate identical diffs across apps
	IgnoreArgocdTracking bool // Default: false - ignore argocd.argoproj.io/* labels/annotations in diffs
}
