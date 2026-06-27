package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	JobQueued     = "queued"
	JobProcessing = "processing"
	JobDone       = "done"
	JobFailed     = "failed"
)

type Job struct {
	ID        string          `json:"job_id"`
	Status    string          `json:"status"`
	KeyID     string          `json:"-"`
	Filename  string          `json:"filename"`
	WAVData   []byte          `json:"-"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	ttl  time.Duration
}

func NewJobStore(ttl time.Duration) *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
		ttl:  ttl,
	}
}

func (s *JobStore) Save(job *Job) {
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
}

func (s *JobStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	return job, ok
}

func (s *JobStore) Delete(id string) {
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
}

func (s *JobStore) StartCleanup(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			cutoff := time.Now().Add(-s.ttl)
			for id, job := range s.jobs {
				if (job.Status == JobDone || job.Status == JobFailed) && job.UpdatedAt.Before(cutoff) {
					delete(s.jobs, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

type JobQueue struct {
	jobs           chan *Job
	workers        int
	store          *JobStore
	whisper        *WhisperClient
	whisperTimeout time.Duration
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

func NewJobQueue(cfg *Config, store *JobStore, whisper *WhisperClient) *JobQueue {
	return &JobQueue{
		jobs:           make(chan *Job, cfg.MaxQueueSize),
		workers:        cfg.NumWorkers,
		store:          store,
		whisper:        whisper,
		whisperTimeout: time.Duration(cfg.WhisperTimeoutSec) * time.Second,
		stopCh:         make(chan struct{}),
	}
}

func (q *JobQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

func (q *JobQueue) Stop() {
	close(q.stopCh)
}

func (q *JobQueue) Enqueue(job *Job) bool {
	select {
	case q.jobs <- job:
		return true
	default:
		return false
	}
}

func (q *JobQueue) worker() {
	defer q.wg.Done()
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

	ctx, cancel := context.WithTimeout(context.Background(), q.whisperTimeout)
	defer cancel()

	result, err := q.whisper.Transcribe(ctx, job.Filename, job.WAVData)
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

func generateJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		log.Printf("WARNING: crypto/rand failed, falling back to timestamp: %v", err)
		return hex.EncodeToString([]byte(time.Now().String()))[:16]
	}
	return hex.EncodeToString(b)
}
