// Package mock provides the deterministic TraceProvider used for development,
// CI and demos. It implements the full provider contract locally and never
// calls an external service. Real platform adapters (national, hubei,
// himarking, antchain, tencent-zxin, nongzhan, lifei) must be implemented
// against their OFFICIAL documentation and registered here once available —
// see specs/06: no endpoint may be invented.
package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/packages/traceprovider"
)

// Compile-time interface check.
var _ traceprovider.Provider = (*Provider)(nil)

// Provider is the mock TraceProvider.
type Provider struct{}

// Name identifies the provider in logs and sync records.
func (p *Provider) Name() string { return "mock" }

// Capabilities follows specs/21 Trace Provider SDK.
func (p *Provider) Capabilities() []string {
	return []string{"PRODUCT", "BATCH", "TRACE_EVENT", "INSPECTION", "CODE", "QUERY", "VERIFY", "EVIDENCE"}
}

// Push deterministically "persists" the payload and returns a stable external id.
func (p *Provider) Push(_ context.Context, task traceprovider.PushTask) (string, error) {
	if task.IdempotencyKey == "" {
		return "", fmt.Errorf("idempotency_key required")
	}
	sum := sha256.Sum256([]byte(task.TenantID + "|" + task.RefType + "|" + task.RefID + "|" + task.IdempotencyKey))
	return "mock-" + hex.EncodeToString(sum[:])[:16], nil
}

// Verify always succeeds for the mock (deterministic by design).
func (p *Provider) Verify(_ context.Context, _ string) (bool, error) {
	return true, nil
}

// Now exists so tests can fake time determinism if needed.
var Now = time.Now
