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
	DedupeDiffs          bool     // Default: true - deduplicate identical diffs across apps
	IgnoreArgocdTracking bool     // Default: false - ignore argocd.argoproj.io/* labels/annotations in diffs
	CollapseThreshold    int      // Default: 3 - collapse all diffs if comment parts exceed this threshold (0 = disabled)
	DestinationClusters  []string // Optional: only include apps targeting these destination cluster names
}
