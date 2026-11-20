// Package production implements the post-harvest chain: harvests, raw material
// batches, processing batches, products, product batches, packages, warehouses,
// inventory and logistics (specs/05.2 chain).
package production

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/crud"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
)

// Module wires production routes.
type Module struct {
	pool     *pgxpool.Pool
	traceSvc *trace.Service
	pub      *events.Publisher
}

func NewModule(pool *pgxpool.Pool, traceSvc *trace.Service, pub *events.Publisher) *Module {
	return &Module{pool: pool, traceSvc: traceSvc, pub: pub}
}

// Mount registers all production endpoints.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	harvests := crud.NewStore(m.pool, crud.Resource{
		Table: "harvests", Perm: "harvest",
		Columns: []crud.Col{
			{Name: "plot_id", Required: true},
			{Name: "crop_batch_id"},
			{Name: "harvested_at"},
			{Name: "quantity", Numeric: true},
			{Name: "unit"},
			{Name: "operator_id"},
			{Name: "device_id"},
			{Name: "mission_id"},
			{Name: "note"},
			{Name: "media"},
			{Name: "status"},
		},
		Search:  []string{"note"},
		Filters: []string{"plot_id", "crop_batch_id", "status", "harvested_after", "harvested_before"},
		Order:   "harvested_at DESC",
	})
	rawBatches := crud.NewStore(m.pool, crud.Resource{
		Table: "raw_material_batches", Alias: "raw-material-batches", Perm: "raw-material-batch",
		Columns: []crud.Col{
			{Name: "code", Required: true},
			{Name: "harvest_id"},
			{Name: "plot_id"},
			{Name: "crop_batch_id"},
			{Name: "quantity", Numeric: true},
			{Name: "unit"},
			{Name: "received_at"},
			{Name: "status"},
			{Name: "note"},
		},
		Search:  []string{"code", "note"},
		Filters: []string{"harvest_id", "plot_id", "status"},
	})
	processingBatches := crud.NewStore(m.pool, crud.Resource{
		Table: "processing_batches", Alias: "processing-batches", Perm: "processing-batch",
		Columns: []crud.Col{
			{Name: "code", Required: true},
			{Name: "raw_material_batch_id", Required: true},
			{Name: "process_type"},
			{Name: "input_quantity", Numeric: true},
			{Name: "output_quantity", Numeric: true},
			{Name: "unit"},
			{Name: "operator_id"},
			{Name: "started_at"},
			{Name: "finished_at"},
			{Name: "parameters"},
			{Name: "status"},
			{Name: "note"},
		},
		Search:  []string{"code", "note"},
		Filters: []string{"raw_material_batch_id", "status", "process_type"},
	})
	products := crud.NewStore(m.pool, crud.Resource{
		Table: "products", Perm: "product",
		Columns: []crud.Col{
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "category"},
			{Name: "spec"},
			{Name: "unit"},
			{Name: "description"},
		},
		Search:  []string{"name", "code", "category"},
		Filters: []string{"category"},
	})
	productBatches := crud.NewStore(m.pool, crud.Resource{
		Table: "product_batches", Alias: "product-batches", Perm: "product-batch",
		Columns: []crud.Col{
			{Name: "code", Required: true},
			{Name: "product_id", Required: true},
			{Name: "processing_batch_id"},
			{Name: "produced_at"},
			{Name: "quantity", Numeric: true},
			{Name: "unit"},
			{Name: "status"},
			{Name: "note"},
		},
		Search:  []string{"code", "note"},
		Filters: []string{"product_id", "processing_batch_id", "status"},
	})
	packages := crud.NewStore(m.pool, crud.Resource{
		Table: "packages", Perm: "package",
		Columns: []crud.Col{
			{Name: "code"},
			{Name: "product_batch_id", Required: true},
			{Name: "quantity", Numeric: true},
			{Name: "unit"},
			{Name: "net_weight", Numeric: true},
			{Name: "gross_weight", Numeric: true},
			{Name: "packed_at"},
			{Name: "status"},
		},
		Search:  []string{"code"},
		Filters: []string{"product_batch_id", "status"},
	})
	warehouses := crud.NewStore(m.pool, crud.Resource{
		Table: "warehouses", Perm: "warehouse",
		Columns: []crud.Col{
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "address"},
			{Name: "location", Geometry: true},
			{Name: "capacity", Numeric: true},
			{Name: "unit"},
		},
		Search: []string{"name", "code", "address"},
	})
	logistics := crud.NewStore(m.pool, crud.Resource{
		Table: "logistics", Perm: "logistics",
		Columns: []crud.Col{
			{Name: "package_id"},
			{Name: "carrier"},
			{Name: "tracking_no"},
			{Name: "origin"},
			{Name: "destination"},
			{Name: "departed_at"},
			{Name: "arrived_at"},
			{Name: "status"},
			{Name: "note"},
		},
		Search:  []string{"carrier", "tracking_no", "origin", "destination"},
		Filters: []string{"package_id", "status"},
	})

	rawBatches.Mount(r, authorize)
	products.Mount(r, authorize)
	productBatches.Mount(r, authorize)
	packages.Mount(r, authorize)
	warehouses.Mount(r, authorize)
	logistics.Mount(r, authorize)

	// harvests: standard CRUD + custom accept action (same subrouter).
	r.Route("/harvests", func(rr chi.Router) {
		rr.With(authorize("harvest", "read")).Get("/", harvests.List)
		rr.With(authorize("harvest", "create")).Post("/", harvests.Create)
		rr.With(authorize("harvest", "read")).Get("/{id}", harvests.Get)
		rr.With(authorize("harvest", "update")).Patch("/{id}", harvests.Update)
		rr.With(authorize("harvest", "delete")).Delete("/{id}", harvests.Delete)
		rr.With(authorize("harvest", "create")).Post("/{id}/accept", m.acceptHarvest)
	})
	// processing-batches: standard CRUD + complete action.
	r.Route("/processing-batches", func(rr chi.Router) {
		rr.With(authorize("processing-batch", "read")).Get("/", processingBatches.List)
		rr.With(authorize("processing-batch", "create")).Post("/", processingBatches.Create)
		rr.With(authorize("processing-batch", "read")).Get("/{id}", processingBatches.Get)
		rr.With(authorize("processing-batch", "update")).Patch("/{id}", processingBatches.Update)
		rr.With(authorize("processing-batch", "delete")).Delete("/{id}", processingBatches.Delete)
		rr.With(authorize("processing-batch", "update")).Post("/{id}/complete", m.completeProcessing)
	})
	r.Route("/inventory", func(rr chi.Router) {
		rr.Use(authorize("inventory", "read"))
		rr.Get("/", m.listInventory)
		rr.With(authorize("inventory", "create")).Post("/movements", m.createMovement)
	})
}
