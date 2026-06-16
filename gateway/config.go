package main

import (
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	GatewayPort     string
	KeysFile        string
	APIKeys         []string
	RateLimitPerKey int
	RateLimitPerIP  int
	RateBurstPerKey int
	RateBurstPerIP  int
	MaxFileSizeMB   int
	AllowedFormats  []string
	WhisperHost     string
	WhisperPort     string
	MaxBodySizeMB   int
	ReadTimeoutSec  int
	WriteTimeoutSec int
	IdleTimeoutSec  int
	ProxyTimeoutSec int
}

func LoadConfig() *Config {
	cfg := &Config{
		GatewayPort:     envStr("PORT", "9090"),
		KeysFile:        envStr("KEYS_FILE", "keys.txt"),
		RateLimitPerKey: envInt("RATE_LIMIT_PER_KEY", 60),
		RateLimitPerIP:  envInt("RATE_LIMIT_PER_IP", 30),
		RateBurstPerKey: envInt("RATE_BURST_PER_KEY", 60),
		RateBurstPerIP:  envInt("RATE_BURST_PER_IP", 30),
		MaxFileSizeMB:   envInt("MAX_FILE_SIZE_MB", 25),
		WhisperHost:     envStr("WHISPER_HOST", "whisper"),
		WhisperPort:     envStr("WHISPER_PORT", "8080"),
		MaxBodySizeMB:   envInt("MAX_BODY_SIZE_MB", 30),
		ReadTimeoutSec:  envInt("READ_TIMEOUT_SEC", 10),
		WriteTimeoutSec: envInt("WRITE_TIMEOUT_SEC", 600),
		IdleTimeoutSec:  envInt("IDLE_TIMEOUT_SEC", 120),
		ProxyTimeoutSec: envInt("PROXY_TIMEOUT_SEC", 600),
		AllowedFormats:  splitComma(envStr("ALLOWED_FORMATS", "wav,mp3,ogg,opus,m4a,flac")),
	}

	cfg.APIKeys = loadKeys(cfg.KeysFile)
	if len(cfg.APIKeys) == 0 {
		log.Println("WARNING: no API keys loaded — auth is disabled")
	} else {
		log.Printf("loaded %d API key(s) from %s", len(cfg.APIKeys), cfg.KeysFile)
	}

	return cfg
}

func loadKeys(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.Printf("error opening keys file %s: %v", path, err)
		return nil
	}
	defer f.Close()

	var keys []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("error reading keys file: %v", err)
	}
	return keys
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
