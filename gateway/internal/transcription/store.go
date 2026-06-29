package transcription

import (
	"sync"
	"time"
)

// JobStore is an in-memory store for job state with TTL-based cleanup of finished jobs.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	ttl  time.Duration
}

// NewJobStore creates a store that retains completed/failed jobs for the given TTL.
func NewJobStore(ttl time.Duration) *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
		ttl:  ttl,
	}
}

// Save persists a job in the store.
func (s *JobStore) Save(job *Job) {
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
}

// Get retrieves a job by ID.
func (s *JobStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	return job, ok
}

// Delete removes a job from the store.
func (s *JobStore) Delete(id string) {
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
}

// StartCleanup periodically removes finished jobs older than the TTL.
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
