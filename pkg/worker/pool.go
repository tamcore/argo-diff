package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tamcore/argo-diff/pkg/logging"
	"github.com/tamcore/argo-diff/pkg/metrics"
)

// Pool manages a pool of workers that process jobs
type Pool struct {
	jobQueue    chan Job
	workerCount int
	jobTimeout  time.Duration
	mu          sync.RWMutex
	closeOnce   sync.Once
	wg          sync.WaitGroup
	processor   JobProcessor
	draining    atomic.Bool
	activeJobs  atomic.Int32
}

// JobProcessor is a function that processes a job
type JobProcessor func(ctx context.Context, job Job) error

// NewPool creates a new worker pool. Each job is processed with a context
// that times out after jobTimeout, so a hung downstream call cannot block
// a worker forever.
func NewPool(workerCount, queueSize int, jobTimeout time.Duration, processor JobProcessor) *Pool {
	return &Pool{
		jobQueue:    make(chan Job, queueSize),
		workerCount: workerCount,
		jobTimeout:  jobTimeout,
		processor:   processor,
	}
}

// Start starts all workers in the pool
func (p *Pool) Start() {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	logging.Info("Worker pool started", "workers", p.workerCount)
}

// Submit adds a job to the queue
// Returns false if the pool is draining or queue is full
func (p *Pool) Submit(job Job) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.draining.Load() {
		return false
	}

	select {
	case p.jobQueue <- job:
		metrics.JobsInQueue.Inc()
		return true
	default:
		return false
	}
}

// Stop gracefully stops the pool. It stops accepting new jobs, lets the
// workers drain all already-accepted (queued) jobs, and waits up to timeout
// for them to finish.
func (p *Pool) Stop(timeout time.Duration) {
	p.draining.Store(true)

	// Close the queue under the write lock so no Submit can race a send
	// against the close.
	p.closeOnce.Do(func() {
		p.mu.Lock()
		close(p.jobQueue)
		p.mu.Unlock()
	})

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logging.Info("Worker pool stopped gracefully")
	case <-time.After(timeout):
		logging.Warn("Worker pool stop timed out", "timeout", timeout)
	}
}

// Status returns the current status of the pool
func (p *Pool) Status() PoolStatus {
	return PoolStatus{
		QueueLength: len(p.jobQueue),
		QueueSize:   cap(p.jobQueue),
		ActiveJobs:  int(p.activeJobs.Load()),
		WorkerCount: p.workerCount,
		Draining:    p.draining.Load(),
	}
}

// IsReady returns true if the pool can accept new jobs
func (p *Pool) IsReady() bool {
	return !p.draining.Load()
}

// PoolStatus represents the current state of the worker pool
type PoolStatus struct {
	QueueLength int  `json:"queue_length"`
	QueueSize   int  `json:"queue_size"`
	ActiveJobs  int  `json:"active_jobs"`
	WorkerCount int  `json:"worker_count"`
	Draining    bool `json:"draining"`
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	workerLog := logging.WithFields("worker_id", id)
	workerLog.Info("Worker started")
	defer workerLog.Info("Worker stopped")

	for job := range p.jobQueue {
		metrics.JobsInQueue.Dec()
		p.activeJobs.Add(1)

		jobLog := logging.WithFields(
			"worker_id", id,
			"repository", job.Repository,
			"pr_number", job.PRNumber,
		)
		jobLog.Info("Processing job")

		startTime := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), p.jobTimeout)
		err := p.processor(ctx, job)
		cancel()
		duration := time.Since(startTime).Seconds()

		p.activeJobs.Add(-1)
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
