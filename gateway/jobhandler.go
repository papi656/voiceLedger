package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func submitJobHandler(cfg *Config, conv *Converter, queue *JobQueue, store *JobStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fh, fileData, valErr := validateAndParseFile(r, cfg)
		if valErr != nil {
			if he, ok := valErr.(*httpError); ok {
				writeHTTPError(w, he)
			} else {
				writeHTTPError(w, &httpError{http.StatusInternalServerError, "internal server error"})
			}
			return
		}

		wavData, convErr := conv.Convert(fh.Filename, fileData)
		if convErr != nil {
			if he, ok := convErr.(*httpError); ok {
				writeHTTPError(w, he)
			} else {
				writeHTTPError(w, &httpError{http.StatusInternalServerError, "internal server error"})
			}
			return
		}

		keyID := "anonymous"
		if v := r.Context().Value(keyIDKey); v != nil {
			if user, ok := v.(UserInfo); ok {
				keyID = user.Sub
			}
		}

		accessToken := r.Header.Get("X-Sheets-Token")

		now := time.Now()
		job := &Job{
			ID:          generateJobID(),
			Status:      JobQueued,
			KeyID:       keyID,
			Filename:    fh.Filename,
			WAVData:     wavData,
			AccessToken: accessToken,
			CreatedAt:   now,
			UpdatedAt:   now,
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

func jobStatusHandler(store *JobStore) http.Handler {
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
