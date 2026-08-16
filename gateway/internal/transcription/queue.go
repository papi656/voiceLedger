package transcription

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"gateway/internal/llm"
	"gateway/internal/sheets"
)

// Transcriber is the interface for sending audio to the transcription service.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, wavData []byte) ([]byte, error)
}

// JobQueue manages a bounded FIFO queue of jobs processed by a fixed worker pool.
type JobQueue struct {
	jobs              chan *Job
	workers           int
	store             *JobStore
	transcriber       Transcriber
	transcribeTimeout time.Duration
	llmClient         *llm.Client
	llmMaxRetries     int
	sheetsClient      *sheets.Client
	sheetsMaxRetries  int
	stopCh            chan struct{}
	Wg                sync.WaitGroup
}

// NewJobQueue creates a queue with the given capacity, worker count, and dependencies.
func NewJobQueue(maxQueueSize, numWorkers int, transcribeTimeout time.Duration, store *JobStore, transcriber Transcriber, llmClient *llm.Client, llmMaxRetries int, sheetsClient *sheets.Client, sheetsMaxRetries int) *JobQueue {
	return &JobQueue{
		jobs:              make(chan *Job, maxQueueSize),
		workers:           numWorkers,
		store:             store,
		transcriber:       transcriber,
		transcribeTimeout: transcribeTimeout,
		llmClient:         llmClient,
		llmMaxRetries:     llmMaxRetries,
		sheetsClient:      sheetsClient,
		sheetsMaxRetries:  sheetsMaxRetries,
		stopCh:            make(chan struct{}),
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

	whisperRaw, err := q.transcriber.Transcribe(ctx, job.Filename, job.WAVData)
	job.WAVData = nil
	job.UpdatedAt = time.Now()

	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		log.Printf("job %s whisper failed: %v", job.ID, err)
		q.store.Save(job)
		return
	}

	// Parse whisper response to get transcription text.
	text, err := parseWhisperText(whisperRaw)
	if err != nil {
		log.Printf("job %s failed to parse whisper response: %v", job.ID, err)
		text = string(whisperRaw) // fallback: use raw response as text
	}

	// Build result with transcription.
	job.Result = &JobResult{Transcription: text}

	// Run LLM extraction with retries — extraction failure fails the job.
	if q.llmClient != nil {
		extraction, llmErr := q.llmClient.ExtractWithRetry(ctx, text, job.Category, q.llmMaxRetries)
		if llmErr != nil {
			job.Status = JobFailed
			job.Error = llmErr.Error()
			job.UpdatedAt = time.Now()
			q.store.Save(job)
			log.Printf("job %s extraction failed, marking job failed: %v", job.ID, llmErr)
			return
		}
		job.Result.Extraction = extraction
		log.Printf("job %s extraction: price=%q place=%q category=%q", job.ID, extraction.Price, extraction.Place, extraction.Category)

		// Append the extraction row to Google Sheets (best-effort with retries
		// and dedupe by job_id — a sheet outage must not fail the job).
		if q.sheetsClient != nil {
			tab := job.Sheet
			if tab == "" {
				tab = q.sheetsClient.DefaultTab
			}
			row := sheets.BuildRow(extraction.Date, extraction.Price, extraction.Place, extraction.Category)
			if err := q.sheetsClient.AppendRowWithRetry(ctx, tab, row, q.sheetsMaxRetries); err != nil {
				log.Printf("job %s google sheets append failed (job still done): %v", job.ID, err)
			} else {
				log.Printf("job %s google sheets export done (tab %q)", job.ID, tab)
			}
		}
	}

	job.Status = JobDone
	job.UpdatedAt = time.Now()
	q.store.Save(job)
	log.Printf("job %s completed", job.ID)
}

// parseWhisperText extracts the "text" field from a whisper JSON response.
func parseWhisperText(raw []byte) (string, error) {
	var resp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Text, nil
}
