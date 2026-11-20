package sync

import "github.com/ouyang6559/CamelliaTraceabilityCloud/packages/traceprovider"

// Provider / PushTask / status constants are re-exported from the public SDK
// package so vendor adapters can implement them without importing internals.
type (
	Provider = traceprovider.Provider
	PushTask = traceprovider.PushTask
)

// Re-exported sync status values (specs/05.10).
const (
	StatusLocalOnly = traceprovider.StatusLocalOnly
	StatusPending   = traceprovider.StatusPending
	StatusSyncing   = traceprovider.StatusSyncing
	StatusSynced    = traceprovider.StatusSynced
	StatusFailed    = traceprovider.StatusFailed
	StatusRetrying  = traceprovider.StatusRetrying
)
