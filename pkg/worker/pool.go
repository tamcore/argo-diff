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
	done        chan struct{}
	wg          sync.WaitGroup
	processor   JobProcessor
	draining    atomic.Bool
	activeJobs  atomic.Int32
}

// JobProcessor is a function that processes a job
type JobProcessor func(ctx context.Context, job Job) error

// NewPool creates a new worker pool
func NewPool(workerCount, queueSize int, processor JobProcessor) *Pool {
	return &Pool{
		jobQueue:    make(chan Job, queueSize),
		workerCount: workerCount,
		done:        make(chan struct{}),
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

// Stop gracefully stops the pool, waiting for in-progress jobs
func (p *Pool) Stop(timeout time.Duration) {
	p.draining.Store(true)
	close(p.done)

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

	for {
		select {
		case <-p.done:
			return
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}

			metrics.JobsInQueue.Dec()
			p.activeJobs.Add(1)

			jobLog := logging.WithFields(
				"worker_id", id,
				"repository", job.Repository,
				"pr_number", job.PRNumber,
			)
			jobLog.Info("Processing job")

			startTime := time.Now()
			err := p.processor(context.Background(), job)
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
}
