package httputil

import (
	"encoding/json"
	"net/http"
)

// HTTPError is an error type that carries an HTTP status code and message.
type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// WriteHTTPError writes a JSON-encoded error to the response with the appropriate status code.
func WriteHTTPError(w http.ResponseWriter, err *HTTPError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Message})
}
