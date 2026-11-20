package middleware

import (
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
)

func newRequestID() string {
	return ulid.Make().String()
}

func panicError() error {
	return apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "internal error")
}
