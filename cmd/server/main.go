package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tamcore/argo-diff/pkg/argocd"
	"github.com/tamcore/argo-diff/pkg/auth"
	"github.com/tamcore/argo-diff/pkg/config"
	"github.com/tamcore/argo-diff/pkg/diff"
	"github.com/tamcore/argo-diff/pkg/github"
	"github.com/tamcore/argo-diff/pkg/matcher"
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
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting argo-diff server")
	log.Printf("Port: %d, Metrics Port: %d", cfg.Port, cfg.MetricsPort)
	log.Printf("Workers: %d, Queue Size: %d", cfg.WorkerCount, cfg.QueueSize)
	log.Printf("Repository Allowlist: %v", cfg.RepoAllowlist)

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
		log.Printf("Metrics server listening on :%d", cfg.MetricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Metrics server error: %v", err)
		}
	}()

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("Server listening on :%d", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	if err := metricsServer.Shutdown(ctx); err != nil {
		log.Printf("Metrics server shutdown error: %v", err)
	}

	close(srv.done)
	close(srv.jobQueue)

	log.Println("Shutdown complete")
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	token, err := auth.ExtractBearerToken(authHeader)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid authorization: %v", err), http.StatusUnauthorized)
		return
	}

	repo, err := s.oidc.ValidateToken(r.Context(), token)
	if err != nil {
		http.Error(w, fmt.Sprintf("Token validation failed: %v", err), http.StatusUnauthorized)
		return
	}

	if !s.cfg.IsRepoAllowed(repo) {
		http.Error(w, "Repository not in allowlist", http.StatusForbidden)
		return
	}

	var payload WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if err := validatePayload(&payload); err != nil {
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
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "accepted",
			"message": fmt.Sprintf("Job queued for %s PR #%d", payload.Repository, payload.PRNumber),
		})
		log.Printf("Queued job for %s PR #%d", payload.Repository, payload.PRNumber)
	default:
		http.Error(w, "Queue full, try again later", http.StatusServiceUnavailable)
		log.Printf("Queue full, rejected job for %s PR #%d", payload.Repository, payload.PRNumber)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.done:
		http.Error(w, "Shutting down", http.StatusServiceUnavailable)
		return
	default:
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (s *Server) worker(id int) {
	log.Printf("Worker %d started", id)
	defer log.Printf("Worker %d stopped", id)

	for {
		select {
		case <-s.done:
			return
		case job, ok := <-s.jobQueue:
			if !ok {
				return
			}

			log.Printf("Worker %d processing job for %s PR #%d", id, job.Repository, job.PRNumber)

			if err := s.processJob(context.Background(), job); err != nil {
				log.Printf("Worker %d error processing job: %v", id, err)
			} else {
				log.Printf("Worker %d completed job for %s PR #%d", id, job.Repository, job.PRNumber)
			}
		}
	}
}

func (s *Server) processJob(ctx context.Context, job worker.Job) error {
	// Parse repository (owner/repo format)
	parts := strings.Split(job.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format: %s", job.Repository)
	}
	owner, repo := parts[0], parts[1]

	// Create GitHub client
	ghClient := github.NewClient(ctx, job.GitHubToken, owner, repo)

	// Create ArgoCD client
	argoClient, err := argocd.NewClient(ctx, job.ArgocdServer, job.ArgocdToken, job.ArgocdInsecure)
	if err != nil {
		// Post error to GitHub
		errorMsg := fmt.Sprintf("## ❌ Error\n\nFailed to connect to ArgoCD: %v", err)
		_ = ghClient.PostComment(ctx, job.PRNumber, errorMsg)
		return fmt.Errorf("create argocd client: %w", err)
	}
	defer argoClient.Close()

	// List all ArgoCD applications
	apps, err := argoClient.ListApplications(ctx)
	if err != nil {
		errorMsg := fmt.Sprintf("## ❌ Error\n\nFailed to list ArgoCD applications: %v", err)
		_ = ghClient.PostComment(ctx, job.PRNumber, errorMsg)
		return fmt.Errorf("list applications: %w", err)
	}

	// Match affected applications
	affectedApps := matcher.MatchApplications(apps, job.Repository, job.ChangedFiles)

	if len(affectedApps) == 0 {
		noChangesMsg := fmt.Sprintf("## ✅ No ArgoCD Applications Affected\n\nNo applications found matching repository `%s` and changed files.", job.Repository)
		return ghClient.PostComment(ctx, job.PRNumber, noChangesMsg)
	}

	log.Printf("Found %d affected applications", len(affectedApps))

	// Generate diffs for each affected application
	var allDiffs []string
	for _, app := range affectedApps {
		appName := app.Name

		// Get manifests for base ref
		baseManifests, err := argoClient.GetManifests(ctx, appName, job.BaseRef)
		if err != nil {
			log.Printf("Warning: failed to get base manifests for %s: %v", appName, err)
			allDiffs = append(allDiffs, fmt.Sprintf("## ⚠️ `%s`\n\nFailed to get base manifests: %v", appName, err))
			continue
		}

		// Get manifests for head ref
		headManifests, err := argoClient.GetManifests(ctx, appName, job.HeadRef)
		if err != nil {
			log.Printf("Warning: failed to get head manifests for %s: %v", appName, err)
			allDiffs = append(allDiffs, fmt.Sprintf("## ⚠️ `%s`\n\nFailed to get head manifests: %v", appName, err))
			continue
		}

		// Generate diff
		diffOutput, err := diff.GenerateDiff(baseManifests, headManifests, appName)
		if err != nil {
			log.Printf("Warning: failed to generate diff for %s: %v", appName, err)
			allDiffs = append(allDiffs, fmt.Sprintf("## ⚠️ `%s`\n\nFailed to generate diff: %v", appName, err))
			continue
		}

		allDiffs = append(allDiffs, diffOutput)
	}

	// Combine all diffs
	header := fmt.Sprintf("# ArgoCD Diff Report\n\n**Workflow:** %s  \n**Changed Files:** %d  \n**Affected Applications:** %d\n\n",
		job.WorkflowName, len(job.ChangedFiles), len(affectedApps))

	finalComment := header + strings.Join(allDiffs, "\n\n---\n\n")

	// Post comment to GitHub
	return ghClient.PostComment(ctx, job.PRNumber, finalComment)
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
