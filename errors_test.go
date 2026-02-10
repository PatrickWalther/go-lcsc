package lcsc

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIErrorError(t *testing.T) {
	err := &APIError{
		StatusCode: 400,
		Code:       400,
		Message:    "bad request",
	}

	text := err.Error()
	if !strings.Contains(text, "400") {
		t.Fatalf("expected status/code in error string, got %q", text)
	}
	if !strings.Contains(strings.ToLower(text), "bad request") {
		t.Fatalf("expected message in error string, got %q", text)
	}
}

func TestAPIErrorUnwrapMapping(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want error
	}{
		{
			name: "bad request",
			err:  &APIError{StatusCode: 400, Code: 400},
			want: ErrInvalidRequest,
		},
		{
			name: "not found",
			err:  &APIError{StatusCode: 404, Code: 404},
			want: ErrNotFound,
		},
		{
			name: "rate limited",
			err:  &APIError{StatusCode: 429, Code: 429},
			want: ErrRateLimited,
		},
		{
			name: "server error",
			err:  &APIError{StatusCode: 500, Code: 500},
			want: ErrServer,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.want) {
				t.Fatalf("expected %v to match %v", tc.err, tc.want)
			}
		})
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {
	errs := []error{ErrInvalidRequest, ErrNotFound, ErrRateLimited, ErrServer}
	for i := range errs {
		for j := range errs {
			if i == j {
				continue
			}
			if errs[i] == errs[j] {
				t.Fatalf("expected sentinel errors at %d and %d to differ", i, j)
			}
		}
	}
}

func TestAPIErrorNoMatch(t *testing.T) {
	err := &APIError{StatusCode: 418, Code: 418, Message: "teapot"}
	if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrServer) {
		t.Fatalf("expected no sentinel match for 418")
	}
}
