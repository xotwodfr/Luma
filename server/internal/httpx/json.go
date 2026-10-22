package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxJSONBody caps every JSON request body.
const MaxJSONBody = 64 << 10

// DecodeJSON reads exactly one JSON object of at most MaxJSONBody bytes, rejecting unknown fields.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return E(http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		}
		if errors.Is(err, io.EOF) {
			return E(http.StatusBadRequest, "invalid_request", "Request body is empty.")
		}
		return E(http.StatusBadRequest, "invalid_request", "Request body is not valid JSON for this endpoint.")
	}
	if dec.More() {
		return E(http.StatusBadRequest, "invalid_request", "Request body must be a single JSON object.")
	}
	return nil
}
