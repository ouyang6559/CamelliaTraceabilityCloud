// Package httpx provides small HTTP helpers shared by all modules.
package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// Respond writes data (or the mapped error envelope) as JSON.
func Respond(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		e := apierr.From(err)
		body := map[string]any{"code": e.Code, "message": e.Message, "request_id": RequestID(r)}
		if e.Details != nil {
			body["details"] = e.Details
		}
		WriteJSON(w, e.Status, body)
		return
	}
	if data == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	WriteJSON(w, http.StatusOK, data)
}

func Created(w http.ResponseWriter, r *http.Request, data any) {
	WriteJSON(w, http.StatusCreated, data)
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func RequestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func WithRequestID(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
}

// DecodeJSON parses the request body strictly into v.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apierr.Invalid("invalid JSON body: %v", err)
	}
	return nil
}

// PageParams reads the unified pagination contract from specs/09: page, page_size.
type PageParams struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func (p PageParams) Offset() int { return (p.Page - 1) * p.PageSize }

func GetPageParams(r *http.Request) PageParams {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size < 1 || size > 200 {
		size = 20
	}
	return PageParams{Page: page, PageSize: size}
}

// PageResult is the unified list envelope.
type PageResult[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func NewPage[T any](items []T, total int64, p PageParams) PageResult[T] {
	if items == nil {
		items = []T{}
	}
	return PageResult[T]{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}
}

// GetTime parses an ISO-8601 query param, returning zero time when absent.
func GetTime(r *http.Request, key string) time.Time {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
