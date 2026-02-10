package lcsc

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidRequest indicates an invalid client request.
	ErrInvalidRequest = errors.New("lcsc: invalid request")

	// ErrNotFound indicates the requested resource was not found.
	ErrNotFound = errors.New("lcsc: not found")

	// ErrRateLimited indicates the API rate limit has been exceeded.
	ErrRateLimited = errors.New("lcsc: rate limit exceeded")

	// ErrServer indicates a server-side API failure.
	ErrServer = errors.New("lcsc: server error")
)

// APIError represents an error returned by the LCSC API.
type APIError struct {
	StatusCode int
	Code       int
	Message    string
	Details    string
}

func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.StatusCode > 0:
		return fmt.Sprintf("lcsc: api error %d (status %d): %s", e.Code, e.StatusCode, e.Message)
	case e.Message != "":
		return fmt.Sprintf("lcsc: api error %d: %s", e.Code, e.Message)
	case e.StatusCode > 0:
		return fmt.Sprintf("lcsc: api error %d (status %d)", e.Code, e.StatusCode)
	default:
		return fmt.Sprintf("lcsc: api error %d", e.Code)
	}
}

// Unwrap enables errors.Is checks for common failure classes.
func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}

	code := e.Code
	if code == 0 {
		code = e.StatusCode
	}

	switch code {
	case 400:
		return ErrInvalidRequest
	case 404:
		return ErrNotFound
	case 429:
		return ErrRateLimited
	default:
		if code >= 500 {
			return ErrServer
		}
	}
	return nil
}
