// Package apierr implements the unified error envelope defined in specs/09-API-CONVENTIONS.md:
// { code, message, details, request_id }.
package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	CodeInvalidArgument  = "INVALID_ARGUMENT"
	CodeUnauthenticated  = "UNAUTHENTICATED"
	CodePermissionDenied = "PERMISSION_DENIED"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeInternal         = "INTERNAL"
	CodeUnavailable      = "UNAVAILABLE"
	CodeRateLimited      = "RATE_LIMITED"
)

// Error is a service error carrying an HTTP status and a stable error code.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func Invalid(format string, args ...any) *Error {
	return New(http.StatusBadRequest, CodeInvalidArgument, format, args...)
}

func Unauthenticated(msg string) *Error {
	return New(http.StatusUnauthorized, CodeUnauthenticated, "%s", msg)
}

func PermissionDenied(msg string) *Error {
	return New(http.StatusForbidden, CodePermissionDenied, "%s", msg)
}

func NotFound(entity string) *Error {
	return New(http.StatusNotFound, CodeNotFound, "%s not found", entity)
}

func Conflict(format string, args ...any) *Error {
	return New(http.StatusConflict, CodeConflict, format, args...)
}

func Internal(err error) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "internal error: " + err.Error()}
}

func Unavailable(format string, args ...any) *Error {
	return New(http.StatusServiceUnavailable, CodeUnavailable, format, args...)
}

func RateLimited() *Error {
	return New(http.StatusTooManyRequests, CodeRateLimited, "rate limit exceeded")
}

// From maps an arbitrary error to *Error; unknown errors become 500 INTERNAL.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}
