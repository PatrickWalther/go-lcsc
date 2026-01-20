package lcsc

import (
	"errors"
	"fmt"
)

var (
	ErrRateLimited        = errors.New("lcsc: rate limit exceeded")
	ErrProductNotFound    = errors.New("lcsc: product not found")
	ErrInternalServer     = errors.New("lcsc: internal server error")
	ErrServiceUnavailable = errors.New("lcsc: service unavailable")
)

// APIError represents an error returned by the LCSC API.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("lcsc: API error %d: %s", e.Code, e.Message)
}

// errorFromCode returns the appropriate error for an HTTP status code.
func errorFromCode(code int, message string) error {
	switch code {
	case 404:
		return ErrProductNotFound
	case 429:
		return ErrRateLimited
	case 500:
		return ErrInternalServer
	case 503:
		return ErrServiceUnavailable
	default:
		return &APIError{Code: code, Message: message}
	}
}
