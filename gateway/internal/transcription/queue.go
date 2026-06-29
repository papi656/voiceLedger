package transcription

import (
	"context"
	"log"
	"sync"
	"time"
)

// Transcriber is the interface for sending audio to the transcription service.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, wavData []byte) ([]byte, error)
}

// JobQueue manages a bounded FIFO queue of jobs processed by a fixed worker pool.
type JobQueue struct {
	jobs           chan *Job
	workers        int
	store          *JobStore
	transcriber    Transcriber
	transcribeTimeout time.Duration
	stopCh         chan struct{}
	Wg             sync.WaitGroup
}

// NewJobQueue creates a queue with the given capacity, worker count, and dependencies.
func NewJobQueue(maxQueueSize, numWorkers int, transcribeTimeout time.Duration, store *JobStore, transcriber Transcriber) *JobQueue {
	return &JobQueue{
		jobs:           make(chan *Job, maxQueueSize),
		workers:        numWorkers,
		store:          store,
		transcriber:    transcriber,
		transcribeTimeout: transcribeTimeout,
		stopCh:         make(chan struct{}),
	}
}

// Start launches the worker goroutines.
func (q *JobQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.Wg.Add(1)
		go q.worker()
	}
}

// Stop signals all workers to drain and exit.
func (q *JobQueue) Stop() {
	close(q.stopCh)
}

// Enqueue adds a job to the queue. Returns false if the queue is full.
func (q *JobQueue) Enqueue(job *Job) bool {
	select {
	case q.jobs <- job:
		return true
	default:
		return false
	}
}

func (q *JobQueue) worker() {
	defer q.Wg.Done()
	for {
		select {
		case <-q.stopCh:
			return
		case job := <-q.jobs:
			q.processJob(job)
		}
	}
}

func (q *JobQueue) processJob(job *Job) {
	job.Status = JobProcessing
	job.UpdatedAt = time.Now()
	q.store.Save(job)

	ctx, cancel := context.WithTimeout(context.Background(), q.transcribeTimeout)
	defer cancel()

	result, err := q.transcriber.Transcribe(ctx, job.Filename, job.WAVData)
	job.UpdatedAt = time.Now()
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		log.Printf("job %s failed: %v", job.ID, err)
	} else {
		job.Status = JobDone
		job.Result = result
		log.Printf("job %s completed", job.ID)
	}
	job.WAVData = nil
	q.store.Save(job)
}
