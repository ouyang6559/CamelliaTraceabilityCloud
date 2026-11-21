// Package tenantref validates that caller-supplied reference IDs belong to the
// caller's tenant. A foreign-key only proves existence, not ownership —
// without these checks a tenant could reference another tenant's objects
// (cross-tenant reference injection, found in the ADR-006 isolation audit).
package tenantref

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
)

// Tables whitelists the physical tables that may be reference-checked, mapping
// the public ref_type (URL/API naming) to the physical table name.
var Tables = map[string]string{
	"farm":               "farms",
	"farms":              "farms",
	"plot":               "plots",
	"plots":              "plots",
	"crop":               "crops",
	"variety":            "varieties",
	"crop_batch":         "crop_batches",
	"crop-batch":         "crop_batches",
	"task":               "agricultural_tasks",
	"agricultural-task":  "agricultural_tasks",
	"input":              "inputs",
	"input_batch":        "input_batches",
	"harvest":            "harvests",
	"raw_material_batch": "raw_material_batches",
	"processing_batch":   "processing_batches",
	"product":            "products",
	"product_batch":      "product_batches",
	"package":            "packages",
	"warehouse":          "warehouses",
	"logistics":          "logistics",
	"trace_event":        "trace_events",
	"evidence":           "evidence",
	"device":             "devices",
	"device_model":       "device_models",
	"mission":            "missions",
	"geofence":           "geofences",
	"user":               "users",
	"external_platform":  "external_platforms",
	"platform":           "external_platforms",
	"quality_report":     "quality_reports",
	"warehouse_id":       "warehouses",
}

// Exists reports whether the referenced row belongs to the tenant.
func Exists(ctx context.Context, pool *pgxpool.Pool, tenantID, refType, refID string) (bool, error) {
	table, ok := Tables[refType]
	if !ok {
		return false, apierr.Invalid("unknown reference type %q", refType)
	}
	if refID == "" {
		return true, nil
	}
	var n int
	// table comes from the whitelist above, never from user input.
	err := pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id=$1 AND id=$2`, table),
		tenantID, refID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MustExist returns an INVALID_ARGUMENT error when the reference is missing or
// owned by another tenant (the exact cause is not disclosed to the caller).
func MustExist(ctx context.Context, pool *pgxpool.Pool, tenantID, refType, refID string) error {
	if refID == "" {
		return nil
	}
	ok, err := Exists(ctx, pool, tenantID, refType, refID)
	if err != nil {
		return err
	}
	if !ok {
		return apierr.Invalid("referenced %s does not exist in your tenant", refType)
	}
	return nil
}

// MustExistAll checks a batch of (refType, refID) pairs, skipping empty ids.
func MustExistAll(ctx context.Context, pool *pgxpool.Pool, tenantID string, refs ...[2]string) error {
	for _, ref := range refs {
		if ref[1] == "" {
			continue
		}
		if err := MustExist(ctx, pool, tenantID, ref[0], ref[1]); err != nil {
			return err
		}
	}
	return nil
}
