package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tamcore/argo-diff/pkg/argocd"
	"github.com/tamcore/argo-diff/pkg/auth"
	"github.com/tamcore/argo-diff/pkg/config"
	"github.com/tamcore/argo-diff/pkg/diff"
	"github.com/tamcore/argo-diff/pkg/github"
	"github.com/tamcore/argo-diff/pkg/logging"
	"github.com/tamcore/argo-diff/pkg/matcher"
	"github.com/tamcore/argo-diff/pkg/metrics"
	"github.com/tamcore/argo-diff/pkg/worker"
)

type WebhookPayload struct {
	GitHubToken    string   `json:"github_token"`
	ArgocdToken    string   `json:"argocd_token"`
	ArgocdServer   string   `json:"argocd_server"`
	ArgocdInsecure bool     `json:"argocd_insecure"`
	Repository     string   `json:"repository"`
	PRNumber       int      `json:"pr_number"`
	BaseRef        string   `json:"base_ref"`
	HeadRef        string   `json:"head_ref"`
	ChangedFiles   []string `json:"changed_files"`
	WorkflowName   string   `json:"workflow_name"`
}

type Server struct {
	cfg      *config.Config
	oidc     *auth.OIDCValidator
	jobQueue chan worker.Job
	done     chan struct{}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		logging.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logging.Init(cfg.LogLevel)

	logging.Info("Starting argo-diff server",
		"port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"workers", cfg.WorkerCount,
		"queue_size", cfg.QueueSize,
		"log_level", cfg.LogLevel,
	)

	srv := &Server{
		cfg:      cfg,
		oidc:     auth.NewOIDCValidator(),
		jobQueue: make(chan worker.Job, cfg.QueueSize),
		done:     make(chan struct{}),
	}

	for i := 0; i < cfg.WorkerCount; i++ {
		go srv.worker(i)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", srv.handleWebhook)
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/ready", srv.handleReady)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler: metricsMux,
	}
	go func() {
		logging.Info("Metrics server started", "port", cfg.MetricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("Metrics server error", "error", err)
			os.Exit(1)
		}
	}()

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		logging.Info("HTTP server started", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logging.Info("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logging.Error("Server shutdown error", "error", err)
	}
	if err := metricsServer.Shutdown(ctx); err != nil {
		logging.Error("Metrics server shutdown error", "error", err)
	}

	close(srv.done)
	close(srv.jobQueue)

	logging.Info("Shutdown complete")
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	requestID := uuid.New().String()
	ctx := logging.WithRequestID(r.Context(), requestID)
	log := logging.FromContext(ctx)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	token, err := auth.ExtractBearerToken(authHeader)
	if err != nil {
		log.Warn("Invalid authorization header", "error", err)
		http.Error(w, fmt.Sprintf("Invalid authorization: %v", err), http.StatusUnauthorized)
		return
	}

	repo, err := s.oidc.ValidateToken(ctx, token)
	if err != nil {
		log.Warn("Token validation failed", "error", err)
		http.Error(w, fmt.Sprintf("Token validation failed: %v", err), http.StatusUnauthorized)
		return
	}

	if !s.cfg.IsRepoAllowed(repo) {
		log.Warn("Repository not in allowlist", "repository", repo)
		http.Error(w, "Repository not in allowlist", http.StatusForbidden)
		return
	}

	var payload WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn("Invalid JSON payload", "error", err)
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if err := validatePayload(&payload); err != nil {
		log.Warn("Invalid payload", "error", err)
		http.Error(w, fmt.Sprintf("Invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if payload.WorkflowName == "" {
		payload.WorkflowName = "ArgoCD Diff"
	}

	job := worker.Job{
		Repository:     payload.Repository,
		PRNumber:       payload.PRNumber,
		BaseRef:        payload.BaseRef,
		HeadRef:        payload.HeadRef,
		ChangedFiles:   payload.ChangedFiles,
		GitHubToken:    payload.GitHubToken,
		WorkflowName:   payload.WorkflowName,
		ArgocdServer:   payload.ArgocdServer,
		ArgocdToken:    payload.ArgocdToken,
		ArgocdInsecure: payload.ArgocdInsecure,
	}

	select {
	case s.jobQueue <- job:
		metrics.JobsInQueue.Inc()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "accepted",
			"message": fmt.Sprintf("Job queued for %s PR #%d", payload.Repository, payload.PRNumber),
		})
		log.Info("Job queued",
			"repository", payload.Repository,
			"pr_number", payload.PRNumber,
			"workflow", payload.WorkflowName,
			"changed_files", len(payload.ChangedFiles),
		)
	default:
		http.Error(w, "Queue full, try again later", http.StatusServiceUnavailable)
		log.Warn("Queue full, job rejected",
			"repository", payload.Repository,
			"pr_number", payload.PRNumber,
		)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.done:
		http.Error(w, "Shutting down", http.StatusServiceUnavailable)
		return
	default:
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (s *Server) worker(id int) {
	workerLog := logging.WithFields("worker_id", id)
	workerLog.Info("Worker started")
	defer workerLog.Info("Worker stopped")

	for {
		select {
		case <-s.done:
			return
		case job, ok := <-s.jobQueue:
			if !ok {
				return
			}

			metrics.JobsInQueue.Dec()
			jobLog := logging.WithFields(
				"worker_id", id,
				"repository", job.Repository,
				"pr_number", job.PRNumber,
			)
			jobLog.Info("Processing job")

			startTime := time.Now()
			err := s.processJob(context.Background(), job)
			duration := time.Since(startTime).Seconds()

			metrics.ProcessingDuration.WithLabelValues(job.Repository).Observe(duration)

			if err != nil {
				metrics.RecordJobFailure(job.Repository)
				jobLog.Error("Job failed", "error", err, "duration_seconds", duration)
			} else {
				metrics.RecordJobSuccess(job.Repository)
				jobLog.Info("Job completed", "duration_seconds", duration)
			}
		}
	}
}

func (s *Server) processJob(ctx context.Context, job worker.Job) error {
	jobLog := logging.WithFields(
		"repository", job.Repository,
		"pr_number", job.PRNumber,
	)

	// Parse repository (owner/repo format)
	parts := strings.Split(job.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format: %s", job.Repository)
	}
	owner, repo := parts[0], parts[1]

	// Create GitHub client
	ghClient := github.NewClient(ctx, job.GitHubToken, owner, repo)

	// Helper to post errors
	postError := func(msg string) {
		errorMsg := fmt.Sprintf("## ❌ Error\n\n%s", msg)
		_ = ghClient.PostComment(ctx, job.PRNumber, errorMsg, job.WorkflowName)
	}

	// Create ArgoCD client
	argoClient, err := argocd.NewClient(ctx, job.ArgocdServer, job.ArgocdToken, job.ArgocdInsecure)
	if err != nil {
		postError(fmt.Sprintf("Failed to connect to ArgoCD: %v", err))
		return fmt.Errorf("create argocd client: %w", err)
	}
	defer func() { _ = argoClient.Close() }()

	// List all ArgoCD applications
	apps, err := argoClient.ListApplications(ctx)
	if err != nil {
		postError(fmt.Sprintf("Failed to list ArgoCD applications: %v", err))
		return fmt.Errorf("list applications: %w", err)
	}

	// Match affected applications
	affectedApps := matcher.MatchApplications(apps, job.Repository, job.ChangedFiles)

	if len(affectedApps) == 0 {
		noChangesMsg := fmt.Sprintf("## ✅ No ArgoCD Applications Affected\n\nNo applications found matching repository `%s` and changed files.", job.Repository)
		return ghClient.PostComment(ctx, job.PRNumber, noChangesMsg, job.WorkflowName)
	}

	jobLog.Info("Found affected applications", "count", len(affectedApps))

	// Generate diffs for each affected application
	var diffResults []*diff.DiffResult
	for _, app := range affectedApps {
		appName := app.Name
		appInfo := diff.NewAppInfo(app, argoClient.Server())

		// Get manifests - handle multi-source apps
		var baseManifests, headManifests []string

		if argocd.IsMultiSource(app) {
			// Multi-source app: create revisions for all sources
			sourceCount := argocd.GetSourceCount(app)
			baseRevisions := make([]argocd.MultiSourceRevision, sourceCount)
			headRevisions := make([]argocd.MultiSourceRevision, sourceCount)

			for i := 0; i < sourceCount; i++ {
				baseRevisions[i] = argocd.MultiSourceRevision{
					Revision:       job.BaseRef,
					SourcePosition: i + 1, // 1-based
				}
				headRevisions[i] = argocd.MultiSourceRevision{
					Revision:       job.HeadRef,
					SourcePosition: i + 1,
				}
			}

			baseManifests, err = argoClient.GetMultiSourceManifests(ctx, appName, baseRevisions)
			if err != nil {
				jobLog.Warn("Failed to get base manifests for multi-source app", "app", appName, "error", err)
				diffResults = append(diffResults, &diff.DiffResult{
					AppInfo:      appInfo,
					ErrorMessage: fmt.Sprintf("Failed to get base manifests: %v", err),
				})
				continue
			}

			headManifests, err = argoClient.GetMultiSourceManifests(ctx, appName, headRevisions)
			if err != nil {
				jobLog.Warn("Failed to get head manifests for multi-source app", "app", appName, "error", err)
				diffResults = append(diffResults, &diff.DiffResult{
					AppInfo:      appInfo,
					ErrorMessage: fmt.Sprintf("Failed to get head manifests: %v", err),
				})
				continue
			}
		} else {
			// Single-source app
			baseManifests, err = argoClient.GetManifests(ctx, appName, job.BaseRef)
			if err != nil {
				jobLog.Warn("Failed to get base manifests", "app", appName, "error", err)
				diffResults = append(diffResults, &diff.DiffResult{
					AppInfo:      appInfo,
					ErrorMessage: fmt.Sprintf("Failed to get base manifests: %v", err),
				})
				continue
			}

			headManifests, err = argoClient.GetManifests(ctx, appName, job.HeadRef)
			if err != nil {
				jobLog.Warn("Failed to get head manifests", "app", appName, "error", err)
				diffResults = append(diffResults, &diff.DiffResult{
					AppInfo:      appInfo,
					ErrorMessage: fmt.Sprintf("Failed to get head manifests: %v", err),
				})
				continue
			}
		}

		// Generate diff
		result, err := diff.GenerateDiff(baseManifests, headManifests, appInfo)
		if err != nil {
			jobLog.Warn("Failed to generate diff", "app", appName, "error", err)
			diffResults = append(diffResults, &diff.DiffResult{
				AppInfo:      appInfo,
				ErrorMessage: fmt.Sprintf("Failed to generate diff: %v", err),
			})
			continue
		}

		diffResults = append(diffResults, result)
	}

	// Create and format the report
	report := diff.NewDiffReport(job.WorkflowName, diffResults)
	finalComment := diff.FormatReport(report)

	// Post comment to GitHub
	return ghClient.PostComment(ctx, job.PRNumber, finalComment, job.WorkflowName)
}

func validatePayload(p *WebhookPayload) error {
	if p.GitHubToken == "" {
		return fmt.Errorf("github_token is required")
	}
	if p.ArgocdToken == "" {
		return fmt.Errorf("argocd_token is required")
	}
	if p.ArgocdServer == "" {
		return fmt.Errorf("argocd_server is required")
	}
	if p.Repository == "" {
		return fmt.Errorf("repository is required")
	}
	if p.PRNumber <= 0 {
		return fmt.Errorf("pr_number must be positive")
	}
	if p.BaseRef == "" {
		return fmt.Errorf("base_ref is required")
	}
	if p.HeadRef == "" {
		return fmt.Errorf("head_ref is required")
	}
	if len(p.ChangedFiles) == 0 {
		return fmt.Errorf("changed_files cannot be empty")
	}
	return nil
}
