package main

import (
	"log"
	"net/http"
)

func main() {
	cfg := LoadConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler())

	// All other routes go through the middleware chain
	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"ok"}`))
	})
	mux.Handle("/", authMiddleware(cfg)(catchAll))

	log.Printf("gateway listening on :%s", cfg.GatewayPort)
	log.Fatal(http.ListenAndServe(":"+cfg.GatewayPort, mux))
}
