package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "argo_diff"

var (
	// JobsTotal counts the total number of processed jobs by repository and status
	JobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "jobs_total",
			Help:      "Total number of diff jobs processed",
		},
		[]string{"repository", "status"},
	)

	// JobsInQueue tracks the current number of jobs waiting in the queue
	JobsInQueue = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "jobs_in_queue",
			Help:      "Current number of jobs in the queue",
		},
	)

	// ProcessingDuration tracks job processing time
	ProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "processing_duration_seconds",
			Help:      "Time spent processing diff jobs",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"repository"},
	)

	// ArgocdAPICalls counts ArgoCD API calls by operation and status
	ArgocdAPICalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "argocd_api_calls_total",
			Help:      "Total number of ArgoCD API calls",
		},
		[]string{"operation", "status"},
	)

	// GithubAPICalls counts GitHub API calls by operation and status
	GithubAPICalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "github_api_calls_total",
			Help:      "Total number of GitHub API calls",
		},
		[]string{"operation", "status"},
	)

	// WebhooksReceived counts incoming webhook requests by repository and result
	WebhooksReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "webhooks_received_total",
			Help:      "Total number of webhook requests received",
		},
		[]string{"repository", "result"},
	)

	// RateLimitHits counts rate limit rejections by repository
	RateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rate_limit_hits_total",
			Help:      "Total number of requests rejected due to rate limiting",
		},
		[]string{"repository"},
	)

	// ApplicationsProcessed counts applications processed per job by repository and application
	ApplicationsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "applications_processed_total",
			Help:      "Total number of ArgoCD applications processed",
		},
		[]string{"repository", "application", "status"},
	)

	// ApplicationDiffs counts diff results by repository, application, and whether changes were detected
	ApplicationDiffs = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "application_diffs_total",
			Help:      "Total number of application diffs generated",
		},
		[]string{"repository", "application", "has_changes"},
	)

	// ApplicationsAffected tracks the number of affected applications per job
	ApplicationsAffected = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "applications_affected_per_job",
			Help:      "Number of ArgoCD applications affected per diff job",
			Buckets:   prometheus.LinearBuckets(0, 5, 10), // 0, 5, 10, 15, ..., 45
		},
		[]string{"repository"},
	)

	// DiffResourceChanges counts the types of resource changes detected
	DiffResourceChanges = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "diff_resource_changes_total",
			Help:      "Total number of resource changes detected in diffs",
		},
		[]string{"repository", "application", "change_type"},
	)
)

// RecordJobSuccess records a successful job completion
func RecordJobSuccess(repository string) {
	JobsTotal.WithLabelValues(repository, "success").Inc()
}

// RecordJobFailure records a failed job
func RecordJobFailure(repository string) {
	JobsTotal.WithLabelValues(repository, "failure").Inc()
}

// RecordArgocdCall records an ArgoCD API call
func RecordArgocdCall(operation string, err error) {
	status := "success"
	if err != nil {
		status = "failure"
	}
	ArgocdAPICalls.WithLabelValues(operation, status).Inc()
}

// RecordGithubCall records a GitHub API call
func RecordGithubCall(operation string, err error) {
	status := "success"
	if err != nil {
		status = "failure"
	}
	GithubAPICalls.WithLabelValues(operation, status).Inc()
}

// RecordWebhookReceived records an incoming webhook request
func RecordWebhookReceived(repository, result string) {
	WebhooksReceived.WithLabelValues(repository, result).Inc()
}

// RecordRateLimitHit records a rate limit rejection
func RecordRateLimitHit(repository string) {
	RateLimitHits.WithLabelValues(repository).Inc()
}

// RecordApplicationProcessed records an application being processed
func RecordApplicationProcessed(repository, application, status string) {
	ApplicationsProcessed.WithLabelValues(repository, application, status).Inc()
}

// RecordApplicationDiff records a diff result for an application
func RecordApplicationDiff(repository, application string, hasChanges bool) {
	changes := "false"
	if hasChanges {
		changes = "true"
	}
	ApplicationDiffs.WithLabelValues(repository, application, changes).Inc()
}

// RecordApplicationsAffected records the number of affected applications in a job
func RecordApplicationsAffected(repository string, count int) {
	ApplicationsAffected.WithLabelValues(repository).Observe(float64(count))
}

// RecordResourceChange records a resource change detected in a diff
func RecordResourceChange(repository, application, changeType string) {
	DiffResourceChanges.WithLabelValues(repository, application, changeType).Inc()
}
