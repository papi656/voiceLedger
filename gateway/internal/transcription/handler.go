package transcription

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"gateway/internal/audio"
	"gateway/internal/config"
	"gateway/internal/httputil"
	"gateway/internal/ratelimit"
	"gateway/internal/sheets"
)

// SubmitJobHandler parses the uploaded file, converts it to WAV, creates a job, and enqueues it.
func SubmitJobHandler(cfg *config.Config, conv *audio.Converter, queue *JobQueue, store *JobStore, sheetsClient *sheets.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fh, fileData, formVals, valErr := audio.ValidateAndParseFile(r, cfg.MaxBodySizeMB, cfg.MaxFileSizeMB, cfg.AllowedFormats)
		if valErr != nil {
			if he, ok := valErr.(*httputil.HTTPError); ok {
				httputil.WriteHTTPError(w, he)
			} else {
				httputil.WriteHTTPError(w, &httputil.HTTPError{http.StatusInternalServerError, "internal server error"})
			}
			return
		}

		wavData, convErr := conv.Convert(fh.Filename, fileData)
		if convErr != nil {
			if he, ok := convErr.(*httputil.HTTPError); ok {
				httputil.WriteHTTPError(w, he)
			} else {
				httputil.WriteHTTPError(w, &httputil.HTTPError{http.StatusInternalServerError, "internal server error"})
			}
			return
		}

		// Optional per-job target tab for Google Sheets export.
		sheet := strings.TrimSpace(formVals["sheet"])
		if sheet != "" {
			if sheetsClient == nil {
				httputil.WriteHTTPError(w, &httputil.HTTPError{http.StatusBadRequest, "google sheets export is disabled"})
				return
			}
			exists, err := sheetsClient.TabExists(r.Context(), sheet)
			if err != nil {
				log.Printf("sheet tab validation skipped (tabs fetch failed): %v", err)
			} else if !exists {
				httputil.WriteHTTPError(w, &httputil.HTTPError{http.StatusBadRequest, "unknown sheet tab: " + sheet})
				return
			}
		}

		now := time.Now()
		job := &Job{
			ID:        GenerateJobID(),
			Status:    JobQueued,
			KeyID:     ratelimit.ClientIP(r),
			Filename:  fh.Filename,
			Category:  strings.TrimSpace(formVals["category"]),
			Sheet:     sheet,
			WAVData:   wavData,
			CreatedAt: now,
			UpdatedAt: now,
		}

		store.Save(job)

		if !queue.Enqueue(job) {
			job.Status = JobFailed
			job.Error = "queue full, try again later"
			job.UpdatedAt = time.Now()
			store.Save(job)
			w.Header().Set("Retry-After", "30")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "queue full, try again later",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"job_id": job.ID,
			"status": JobQueued,
		})
	})
}

// ListSheetsHandler returns the available Google Sheets tabs so the UI can let
// the user pick where a job's row should be appended.
func ListSheetsHandler(sheetsClient *sheets.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"enabled":    false,
			"default":    "",
			"tabs":       []string{},
			"categories": []string{},
		}
		if sheetsClient != nil {
			tabs, err := sheetsClient.Tabs(r.Context())
			if err != nil {
				httputil.WriteHTTPError(w, &httputil.HTTPError{http.StatusInternalServerError, "failed to fetch sheet tabs"})
				return
			}
			resp["enabled"] = true
			resp["default"] = sheetsClient.DefaultTab
			resp["tabs"] = tabs
			if cats, err := sheetsClient.TypeCategories(r.Context(), sheetsClient.DefaultTab); err == nil {
				resp["categories"] = cats
			} else {
				log.Printf("failed to fetch sheet type categories: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	})
}

// JobStatusHandler returns the current status and result of a job by ID.
func JobStatusHandler(store *JobStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/jobs/")
		jobID := strings.TrimSuffix(path, "/")

		if jobID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing job ID"})
			return
		}

		job, ok := store.Get(jobID)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "job not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(job)
	})
}
