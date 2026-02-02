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
	"github.com/tamcore/argo-diff/pkg/ratelimit"
	"github.com/tamcore/argo-diff/pkg/worker"
)

type WebhookPayload struct {
	GitHubToken  string   `json:"github_token"`
	ArgocdToken  string   `json:"argocd_token"`
	Repository   string   `json:"repository"`
	PRNumber     int      `json:"pr_number"`
	BaseRef      string   `json:"base_ref"`
	HeadRef      string   `json:"head_ref"`
	ChangedFiles []string `json:"changed_files"`
	WorkflowName string   `json:"workflow_name"`
}

type Server struct {
	cfg     *config.Config
	oidc    *auth.OIDCValidator
	pool    *worker.Pool
	limiter *ratelimit.Limiter
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
		"rate_limit_per_repo", cfg.RateLimitPerRepo,
		"argocd_server", cfg.ArgocdServer,
		"argocd_insecure", cfg.ArgocdInsecure,
	)

	srv := &Server{
		cfg:  cfg,
		oidc: auth.NewOIDCValidator(),
	}

	// Create rate limiter if enabled
	if cfg.RateLimitPerRepo > 0 {
		srv.limiter = ratelimit.NewLimiter(cfg.RateLimitPerRepo, time.Minute)
	}

	// Create and start worker pool
	srv.pool = worker.NewPool(cfg.WorkerCount, cfg.QueueSize, srv.processJob)
	srv.pool.Start()

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
		ReadTimeout:  5 * time.Minute, // Increased for sync processing
		WriteTimeout: 5 * time.Minute, // Increased for sync processing
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

	// Stop worker pool gracefully
	srv.pool.Stop(25 * time.Second)

	// Stop rate limiter
	if srv.limiter != nil {
		srv.limiter.Stop()
	}

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

	// Check rate limit
	if s.limiter != nil && !s.limiter.Allow(repo) {
		log.Warn("Rate limit exceeded", "repository", repo)
		http.Error(w, "Rate limit exceeded, try again later", http.StatusTooManyRequests)
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
		ArgocdServer:   s.cfg.ArgocdServer,
		ArgocdToken:    payload.ArgocdToken,
		ArgocdInsecure: s.cfg.ArgocdInsecure,
	}

	// Check if sync processing is requested
	syncMode := r.URL.Query().Get("sync") == "true"

	if syncMode {
		// Process synchronously - this keeps the connection open
		// and the GitHub token valid until we're done
		log.Info("Processing job synchronously",
			"repository", payload.Repository,
			"pr_number", payload.PRNumber,
			"workflow", payload.WorkflowName,
			"changed_files", len(payload.ChangedFiles),
		)

		if err := s.processJob(ctx, job); err != nil {
			log.Error("Sync job failed",
				"repository", payload.Repository,
				"pr_number", payload.PRNumber,
				"error", err,
			)
			http.Error(w, fmt.Sprintf("Job failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "completed",
			"message": fmt.Sprintf("Job completed for %s PR #%d", payload.Repository, payload.PRNumber),
		})
		log.Info("Sync job completed",
			"repository", payload.Repository,
			"pr_number", payload.PRNumber,
		)
	} else if s.pool.Submit(job) {
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
	} else {
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
	if !s.pool.IsReady() {
		http.Error(w, "Shutting down", http.StatusServiceUnavailable)
		return
	}

	status := s.pool.Status()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ready",
		"queue_length": status.QueueLength,
		"queue_size":   status.QueueSize,
		"active_jobs":  status.ActiveJobs,
		"workers":      status.WorkerCount,
	})
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

// Validation constants
const (
	maxRepositoryLength   = 256
	maxRefLength          = 256
	maxWorkflowNameLength = 128
	maxChangedFiles       = 1000
	maxFilePathLength     = 512
)

func validatePayload(p *WebhookPayload) error {
	if p.GitHubToken == "" {
		return fmt.Errorf("github_token is required")
	}
	if p.ArgocdToken == "" {
		return fmt.Errorf("argocd_token is required")
	}
	if p.Repository == "" {
		return fmt.Errorf("repository is required")
	}
	if len(p.Repository) > maxRepositoryLength {
		return fmt.Errorf("repository exceeds maximum length of %d", maxRepositoryLength)
	}
	if !isValidRepository(p.Repository) {
		return fmt.Errorf("repository must be in format 'owner/repo'")
	}
	if p.PRNumber <= 0 {
		return fmt.Errorf("pr_number must be positive")
	}
	if p.PRNumber > 1000000 {
		return fmt.Errorf("pr_number exceeds maximum value")
	}
	if p.BaseRef == "" {
		return fmt.Errorf("base_ref is required")
	}
	if len(p.BaseRef) > maxRefLength {
		return fmt.Errorf("base_ref exceeds maximum length of %d", maxRefLength)
	}
	if p.HeadRef == "" {
		return fmt.Errorf("head_ref is required")
	}
	if len(p.HeadRef) > maxRefLength {
		return fmt.Errorf("head_ref exceeds maximum length of %d", maxRefLength)
	}
	if len(p.ChangedFiles) == 0 {
		return fmt.Errorf("changed_files cannot be empty")
	}
	if len(p.ChangedFiles) > maxChangedFiles {
		return fmt.Errorf("changed_files exceeds maximum of %d files", maxChangedFiles)
	}
	for _, file := range p.ChangedFiles {
		if len(file) > maxFilePathLength {
			return fmt.Errorf("file path exceeds maximum length of %d", maxFilePathLength)
		}
	}
	if len(p.WorkflowName) > maxWorkflowNameLength {
		return fmt.Errorf("workflow_name exceeds maximum length of %d", maxWorkflowNameLength)
	}
	return nil
}

func isValidRepository(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	owner, name := parts[0], parts[1]
	if owner == "" || name == "" {
		return false
	}
	// Basic check for valid characters (alphanumeric, dash, underscore, dot)
	for _, part := range parts {
		for _, c := range part {
			isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			isDigit := c >= '0' && c <= '9'
			isSpecial := c == '-' || c == '_' || c == '.'
			if !isAlpha && !isDigit && !isSpecial {
				return false
			}
		}
	}
	return true
}
