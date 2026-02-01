package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	processor := func(ctx context.Context, job Job) error { return nil }
	pool := NewPool(3, 10, processor)

	if pool == nil {
		t.Fatal("expected pool to be non-nil")
	}
	if pool.workerCount != 3 {
		t.Errorf("expected workerCount=3, got %d", pool.workerCount)
	}
	if cap(pool.jobQueue) != 10 {
		t.Errorf("expected queue capacity=10, got %d", cap(pool.jobQueue))
	}
}

func TestPoolSubmitAndProcess(t *testing.T) {
	var processed atomic.Int32

	processor := func(ctx context.Context, job Job) error {
		processed.Add(1)
		return nil
	}

	pool := NewPool(2, 10, processor)
	pool.Start()
	defer pool.Stop(time.Second)

	// Submit jobs
	for i := 0; i < 5; i++ {
		job := Job{
			Repository: "test/repo",
			PRNumber:   i,
		}
		if !pool.Submit(job) {
			t.Errorf("failed to submit job %d", i)
		}
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	if processed.Load() != 5 {
		t.Errorf("expected 5 processed jobs, got %d", processed.Load())
	}
}

func TestPoolSubmitWhenDraining(t *testing.T) {
	processor := func(ctx context.Context, job Job) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	pool := NewPool(1, 5, processor)
	pool.Start()

	// Mark as draining
	pool.draining.Store(true)

	job := Job{Repository: "test/repo", PRNumber: 1}
	if pool.Submit(job) {
		t.Error("expected Submit to return false when draining")
	}

	pool.Stop(time.Second)
}

func TestPoolSubmitQueueFull(t *testing.T) {
	// Create pool with small queue and slow processor
	processor := func(ctx context.Context, job Job) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	pool := NewPool(1, 2, processor)
	pool.Start()
	defer pool.Stop(time.Second)

	// Fill the queue
	for i := 0; i < 3; i++ {
		pool.Submit(Job{Repository: "test/repo", PRNumber: i})
	}

	// Queue should be full now
	if pool.Submit(Job{Repository: "test/repo", PRNumber: 999}) {
		t.Error("expected Submit to return false when queue is full")
	}
}

func TestPoolStatus(t *testing.T) {
	processor := func(ctx context.Context, job Job) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	pool := NewPool(2, 10, processor)
	pool.Start()
	defer pool.Stop(time.Second)

	// Submit a job
	pool.Submit(Job{Repository: "test/repo", PRNumber: 1})

	// Check status
	status := pool.Status()
	if status.QueueSize != 10 {
		t.Errorf("expected QueueSize=10, got %d", status.QueueSize)
	}
	if status.WorkerCount != 2 {
		t.Errorf("expected WorkerCount=2, got %d", status.WorkerCount)
	}
	if status.Draining {
		t.Error("expected Draining=false")
	}
}

func TestPoolIsReady(t *testing.T) {
	processor := func(ctx context.Context, job Job) error { return nil }
	pool := NewPool(1, 5, processor)
	pool.Start()

	if !pool.IsReady() {
		t.Error("expected IsReady=true initially")
	}

	pool.draining.Store(true)
	if pool.IsReady() {
		t.Error("expected IsReady=false when draining")
	}

	pool.Stop(time.Second)
}

func TestPoolGracefulStop(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	processor := func(ctx context.Context, job Job) error {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	pool := NewPool(1, 5, processor)
	pool.Start()

	// Submit a job that will be processing when we stop
	pool.Submit(Job{Repository: "test/repo", PRNumber: 1})

	// Give it time to pick up the job
	time.Sleep(10 * time.Millisecond)

	// Stop should wait for job to complete
	pool.Stop(time.Second)

	// Verify job completed
	wg.Wait()
}

func TestPoolStopTimeout(t *testing.T) {
	processor := func(ctx context.Context, job Job) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	pool := NewPool(1, 5, processor)
	pool.Start()

	// Submit a slow job
	pool.Submit(Job{Repository: "test/repo", PRNumber: 1})
	time.Sleep(10 * time.Millisecond)

	// Stop with short timeout should timeout
	start := time.Now()
	pool.Stop(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("Stop took too long: %v", elapsed)
	}
}

func TestPoolConcurrentSubmit(t *testing.T) {
	var processed atomic.Int32

	processor := func(ctx context.Context, job Job) error {
		processed.Add(1)
		return nil
	}

	pool := NewPool(4, 100, processor)
	pool.Start()
	defer pool.Stop(time.Second)

	// Concurrent submits
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pool.Submit(Job{Repository: "test/repo", PRNumber: n})
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	if processed.Load() != 50 {
		t.Errorf("expected 50 processed jobs, got %d", processed.Load())
	}
}
