package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config holds all gateway configuration values sourced from environment variables.
type Config struct {
	GatewayPort           string
	CORSAllowedOrigins    string
	RateLimitPerIP        int
	RateBurstPerIP        int
	MaxFileSizeMB         int
	AllowedFormats        []string
	WhisperHost           string
	WhisperPort           string
	MaxBodySizeMB         int
	ReadTimeoutSec        int
	WriteTimeoutSec       int
	IdleTimeoutSec        int
	WhisperTimeoutSec     int
	ConvertTimeoutSec     int
	FFMPEGPath            string
	NumWorkers            int
	MaxQueueSize          int
	JobTTLSec             int
	JobCleanupIntervalSec int
	LLMHost               string
	LLMPort               string
	LLMTimeoutSec         int
}

// Load reads environment variables and returns a populated Config with sane defaults.
func Load() *Config {
	return &Config{
		GatewayPort:           envStr("PORT", "9090"),
		CORSAllowedOrigins:    envStr("CORS_ALLOWED_ORIGINS", "*"),
		RateLimitPerIP:        envInt("RATE_LIMIT_PER_IP", 30),
		RateBurstPerIP:        envInt("RATE_BURST_PER_IP", 30),
		MaxFileSizeMB:         envInt("MAX_FILE_SIZE_MB", 25),
		WhisperHost:           envStr("WHISPER_HOST", "whisper"),
		WhisperPort:           envStr("WHISPER_PORT", "8080"),
		MaxBodySizeMB:         envInt("MAX_BODY_SIZE_MB", 30),
		ReadTimeoutSec:        envInt("READ_TIMEOUT_SEC", 10),
		WriteTimeoutSec:       envInt("WRITE_TIMEOUT_SEC", 30),
		IdleTimeoutSec:        envInt("IDLE_TIMEOUT_SEC", 120),
		WhisperTimeoutSec:     envInt("WHISPER_TIMEOUT_SEC", 600),
		ConvertTimeoutSec:     envInt("CONVERT_TIMEOUT_SEC", 120),
		FFMPEGPath:            envStr("FFMPEG_PATH", "ffmpeg"),
		NumWorkers:            envInt("NUM_WORKERS", 1),
		MaxQueueSize:          envInt("MAX_QUEUE_SIZE", 50),
		JobTTLSec:             envInt("JOB_TTL_SEC", 3600),
		JobCleanupIntervalSec: envInt("JOB_CLEANUP_INTERVAL_SEC", 300),
		LLMHost:               envStr("LLM_HOST", "llama"),
		LLMPort:               envStr("LLM_PORT", "8081"),
		LLMTimeoutSec:         envInt("LLM_TIMEOUT_SEC", 120),
		AllowedFormats:        splitComma(envStr("ALLOWED_FORMATS", "wav,mp3,ogg,opus,m4a,flac")),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
		log.Printf("invalid value for %s=%q, using default %d", key, v, fallback)
	}
	return fallback
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
