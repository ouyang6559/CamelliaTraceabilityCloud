// Package traceprovider is the public Trace Provider SDK (specs/21): any
// compliant third-party traceability platform implements Provider. Vendor
// adapters live in adapters/trace/ and are registered by the sync module.
package traceprovider

import "context"

// SyncStatus values (specs/05.10).
const (
	StatusLocalOnly = "LOCAL_ONLY"
	StatusPending   = "PENDING"
	StatusSyncing   = "SYNCING"
	StatusSynced    = "SYNCED"
	StatusFailed    = "FAILED"
	StatusRetrying  = "RETRYING"
)

// PushTask is the unit delivered to providers.
type PushTask struct {
	TaskID         string
	TenantID       string
	RefType        string
	RefID          string
	Action         string
	IdempotencyKey string
	Payload        map[string]any
}

// Provider is the TraceProvider contract. Implementations must be idempotent,
// retryable, observable and auditable; credentials come from configuration,
// never from code (specs/06.10, specs/08.7).
type Provider interface {
	// Name identifies the provider in logs and sync records.
	Name() string
	// Capabilities lists supported actions per specs/21:
	// PRODUCT, BATCH, TRACE_EVENT, INSPECTION, CODE, QUERY, VERIFY, EVIDENCE.
	Capabilities() []string
	// Push delivers one sync task payload and returns the external id.
	Push(ctx context.Context, task PushTask) (externalID string, err error)
	// Verify checks the sync state of a previously pushed object.
	Verify(ctx context.Context, externalID string) (verified bool, err error)
}
