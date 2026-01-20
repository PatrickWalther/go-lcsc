package lcsc

import (
	"errors"
	"testing"
)

// TestAPIErrorError tests the Error method of APIError.
func TestAPIErrorError(t *testing.T) {
	err := &APIError{
		Code:    400,
		Message: "bad request",
	}

	expected := "lcsc: API error 400: bad request"
	if err.Error() != expected {
		t.Errorf("expected error string %q, got %q", expected, err.Error())
	}
}

// TestAPIErrorErrorWithDifferentCodes tests Error method with various codes.
func TestAPIErrorErrorWithDifferentCodes(t *testing.T) {
	testCases := []struct {
		code    int
		message string
	}{
		{400, "invalid input"},
		{401, "unauthorized"},
		{403, "forbidden"},
		{404, "not found"},
		{500, "internal error"},
	}

	for _, tc := range testCases {
		err := &APIError{
			Code:    tc.code,
			Message: tc.message,
		}

		if !contains(err.Error(), "lcsc") {
			t.Errorf("error string should contain 'lcsc'")
		}
		if !contains(err.Error(), "API error") {
			t.Errorf("error string should contain 'API error'")
		}
		if !contains(err.Error(), tc.message) {
			t.Errorf("error string should contain message %q", tc.message)
		}
	}
}

// TestErrorFromCodeNotFound tests errorFromCode for 404.
func TestErrorFromCodeNotFound(t *testing.T) {
	err := errorFromCode(404, "product not found")

	if err != ErrProductNotFound {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// TestErrorFromCodeRateLimit tests errorFromCode for 429.
func TestErrorFromCodeRateLimit(t *testing.T) {
	err := errorFromCode(429, "rate limit exceeded")

	if err != ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

// TestErrorFromCodeInternalServer tests errorFromCode for 500.
func TestErrorFromCodeInternalServer(t *testing.T) {
	err := errorFromCode(500, "internal server error")

	if err != ErrInternalServer {
		t.Fatalf("expected ErrInternalServer, got %v", err)
	}
}

// TestErrorFromCodeServiceUnavailable tests errorFromCode for 503.
func TestErrorFromCodeServiceUnavailable(t *testing.T) {
	err := errorFromCode(503, "service unavailable")

	if err != ErrServiceUnavailable {
		t.Fatalf("expected ErrServiceUnavailable, got %v", err)
	}
}

// TestErrorFromCodeUnknown tests errorFromCode for unknown codes.
func TestErrorFromCodeUnknown(t *testing.T) {
	err := errorFromCode(999, "unknown error")

	// Should return a generic APIError
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}

	if apiErr.Code != 999 {
		t.Errorf("expected code 999, got %d", apiErr.Code)
	}

	if apiErr.Message != "unknown error" {
		t.Errorf("expected message 'unknown error', got %q", apiErr.Message)
	}
}

// TestErrProductNotFoundString tests that the product not found error has correct string.
func TestErrProductNotFoundString(t *testing.T) {
	errStr := ErrProductNotFound.Error()

	if !contains(errStr, "product") || !contains(errStr, "not") || !contains(errStr, "found") {
		t.Errorf("unexpected error string: %s", errStr)
	}
}

// TestErrRateLimitedString tests that the rate limit error has correct string.
func TestErrRateLimitedString(t *testing.T) {
	errStr := ErrRateLimited.Error()

	if !contains(errStr, "rate") {
		t.Errorf("unexpected error string: %s", errStr)
	}
}

// TestErrInternalServerString tests that the internal server error has correct string.
func TestErrInternalServerString(t *testing.T) {
	errStr := ErrInternalServer.Error()

	if !contains(errStr, "internal") || !contains(errStr, "server") {
		t.Errorf("unexpected error string: %s", errStr)
	}
}

// TestErrServiceUnavailableString tests that the service unavailable error has correct string.
func TestErrServiceUnavailableString(t *testing.T) {
	errStr := ErrServiceUnavailable.Error()

	if !contains(errStr, "service") || !contains(errStr, "unavailable") {
		t.Errorf("unexpected error string: %s", errStr)
	}
}

// TestErrorsAreDistinct tests that different errors are distinct.
func TestErrorsAreDistinct(t *testing.T) {
	errors := []error{
		ErrProductNotFound,
		ErrRateLimited,
		ErrInternalServer,
		ErrServiceUnavailable,
	}

	for i, err1 := range errors {
		for j, err2 := range errors {
			if i != j && err1 == err2 {
				t.Errorf("errors at indices %d and %d should be different", i, j)
			}
		}
	}
}

// TestAPIErrorIsError tests that APIError implements error interface.
func TestAPIErrorIsError(t *testing.T) {
	var err error = &APIError{Code: 400, Message: "test"}

	if err == nil {
		t.Fatal("expected non-nil error")
	}

	if err.Error() == "" {
		t.Fatal("expected non-empty error string")
	}
}

// TestErrorFromCodeWithEmptyMessage tests errorFromCode with empty message.
func TestErrorFromCodeWithEmptyMessage(t *testing.T) {
	err := errorFromCode(404, "")

	if err != ErrProductNotFound {
		t.Errorf("should return standard error for 404 regardless of message")
	}
}

// TestAPIErrorWithEmptyMessage tests APIError with empty message.
func TestAPIErrorWithEmptyMessage(t *testing.T) {
	err := &APIError{
		Code:    400,
		Message: "",
	}

	errStr := err.Error()

	// Should still produce a valid error string
	if !contains(errStr, "400") {
		t.Errorf("error string should contain code: %s", errStr)
	}
}
