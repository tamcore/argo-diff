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
