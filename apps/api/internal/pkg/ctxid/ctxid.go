// Package ctxid carries the authenticated identity through request contexts.
package ctxid

import (
	"context"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
)

type key string

const (
	userKey   key = "user"
	claimsKey key = "claims"
)

// Claims is the JWT payload.
type Claims struct {
	UserID      string   `json:"sub"`
	TenantID    string   `json:"tid"`
	Username    string   `json:"username"`
	DisplayName string   `json:"name"`
	Roles       []string `json:"roles"`
	Perms       []string `json:"perms"` // e.g. "farm:read", "*:*"
}

// Identity is the resolved user attached by the auth middleware.
type Identity struct {
	UserID      string
	TenantID    string
	Username    string
	DisplayName string
	Roles       []string
	Perms       []string
}

// WithIdentity stores the identity in the context.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	ctx = context.WithValue(ctx, userKey, id)
	return context.WithValue(ctx, claimsKey, id)
}

// From returns the identity; ok=false for unauthenticated contexts.
func From(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(userKey).(Identity)
	return v, ok
}

// Must returns the identity or an unauthenticated error.
func Must(ctx context.Context) (Identity, error) {
	v, ok := From(ctx)
	if !ok {
		return Identity{}, apierr.Unauthenticated("missing or invalid token")
	}
	return v, nil
}

// HasPerm checks resource:action against the identity's permission set (supports "*:*", "res:*").
func (i Identity) HasPerm(resource, action string) bool {
	want := resource + ":" + action
	for _, p := range i.Perms {
		if p == want || p == resource+":*" || p == "*:"+action || p == "*:*" {
			return true
		}
	}
	return false
}

// DisplayNameOrUsername prefers the display name for audit trails.
func (i Identity) DisplayNameOrUsername() string {
	if i.DisplayName != "" {
		return i.DisplayName
	}
	return i.Username
}
