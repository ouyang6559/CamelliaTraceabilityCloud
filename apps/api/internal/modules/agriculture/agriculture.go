// Package agriculture implements farms, plots, crops, varieties, crop batches,
// agricultural tasks, inputs and disease records (specs/04, specs/11.2).
package agriculture

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/crud"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
)

// Module wires agriculture routes.
type Module struct {
	pool     *pgxpool.Pool
	traceSvc *trace.Service
	pub      *events.Publisher
}

func NewModule(pool *pgxpool.Pool, traceSvc *trace.Service, pub *events.Publisher) *Module {
	return &Module{pool: pool, traceSvc: traceSvc, pub: pub}
}

// Mount registers all agriculture endpoints; authorize wraps permission checks.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	farms := crud.NewStore(m.pool, crud.Resource{
		Table: "farms", Perm: "farm",
		Columns: []crud.Col{
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "description"},
			{Name: "address"},
			{Name: "location", Geometry: true},
			{Name: "boundary", Geometry: true},
			{Name: "area_m2", Numeric: true},
			{Name: "metadata"},
		},
		Search:  []string{"name", "code", "address"},
		Filters: []string{"code", "created_after", "created_before"},
		Order:   "created_at DESC",
	})
	plots := crud.NewStore(m.pool, crud.Resource{
		Table: "plots", Perm: "plot",
		Columns: []crud.Col{
			{Name: "farm_id", Required: true},
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "description"},
			{Name: "boundary", Geometry: true},
			{Name: "area_m2", Numeric: true},
			{Name: "soil_type"},
			{Name: "planted_year"},
			{Name: "status"},
			{Name: "accuracy_m", Numeric: true},
			{Name: "source"},
			{Name: "captured_at"},
			{Name: "metadata"},
		},
		Search:  []string{"name", "code", "soil_type"},
		Filters: []string{"farm_id", "status", "created_after", "created_before"},
	})
	crops := crud.NewStore(m.pool, crud.Resource{
		Table: "crops", Perm: "crop",
		Columns: []crud.Col{
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "category"},
			{Name: "description"},
		},
		Search:  []string{"name", "code", "category"},
		Filters: []string{"category"},
	})
	varieties := crud.NewStore(m.pool, crud.Resource{
		Table: "varieties", Perm: "variety",
		Columns: []crud.Col{
			{Name: "crop_id", Required: true},
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "description"},
			{Name: "traits"},
		},
		Search:  []string{"name", "code"},
		Filters: []string{"crop_id"},
	})
	cropBatches := crud.NewStore(m.pool, crud.Resource{
		Table: "crop_batches", Alias: "crop-batches", Perm: "crop-batch",
		Columns: []crud.Col{
			{Name: "plot_id", Required: true},
			{Name: "crop_id", Required: true},
			{Name: "variety_id"},
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "planted_at"},
			{Name: "area_m2", Numeric: true},
			{Name: "status"},
			{Name: "metadata"},
		},
		Search:  []string{"name", "code"},
		Filters: []string{"plot_id", "crop_id", "variety_id", "status", "created_after", "created_before"},
	})
	inputs := crud.NewStore(m.pool, crud.Resource{
		Table: "inputs", Perm: "input",
		Columns: []crud.Col{
			{Name: "name", Required: true},
			{Name: "code"},
			{Name: "category"},
			{Name: "unit"},
			{Name: "spec"},
			{Name: "description"},
		},
		Search:  []string{"name", "code", "category"},
		Filters: []string{"category"},
	})
	inputBatches := crud.NewStore(m.pool, crud.Resource{
		Table: "input_batches", Alias: "input-batches", Perm: "input-batch",
		Columns: []crud.Col{
			{Name: "input_id", Required: true},
			{Name: "code", Required: true},
			{Name: "supplier"},
			{Name: "quantity", Numeric: true},
			{Name: "unit"},
			{Name: "produced_at"},
			{Name: "expired_at"},
			{Name: "status"},
		},
		Search:  []string{"code", "supplier"},
		Filters: []string{"input_id", "status"},
	})
	diseaseRecords := crud.NewStore(m.pool, crud.Resource{
		Table: "disease_records", Alias: "disease-records", Perm: "disease-record",
		Columns: []crud.Col{
			{Name: "plot_id"},
			{Name: "crop_batch_id"},
			{Name: "category"},
			{Name: "name_zh", Required: true},
			{Name: "name_en"},
			{Name: "severity"},
			{Name: "location", Geometry: true},
			{Name: "discovered_at"},
			{Name: "status"},
			{Name: "treatment"},
			{Name: "media"},
		},
		Search:  []string{"name_zh", "name_en", "treatment"},
		Filters: []string{"plot_id", "crop_batch_id", "category", "status", "severity"},
	})

	farms.Mount(r, authorize)
	plots.Mount(r, authorize)
	crops.Mount(r, authorize)
	varieties.Mount(r, authorize)
	cropBatches.Mount(r, authorize)
	inputs.Mount(r, authorize)
	inputBatches.Mount(r, authorize)
	diseaseRecords.Mount(r, authorize)

	r.Route("/agricultural-tasks", func(rr chi.Router) {
		rr.Use(authorize("task", "read"))
		rr.Get("/", m.listTasks)
		rr.With(authorize("task", "create")).Post("/", m.createTask)
		rr.Get("/{id}", m.getTask)
		rr.With(authorize("task", "update")).Patch("/{id}", m.updateTask)
		rr.With(authorize("task", "delete")).Delete("/{id}", m.deleteTask)
		rr.With(authorize("task", "update")).Post("/{id}/start", m.startTask)
		rr.With(authorize("task", "update")).Post("/{id}/complete", m.completeTask)
		rr.Get("/{id}/records", m.listTaskRecords)
		rr.With(authorize("task-record", "create")).Post("/{id}/records", m.createTaskRecord)
	})
	r.Route("/input-usages", func(rr chi.Router) {
		rr.Use(authorize("input-usage", "read"))
		rr.Get("/", m.listInputUsages)
		rr.With(authorize("input-usage", "create")).Post("/", m.createInputUsage)
	})
	r.With(authorize("crop", "read")).Get("/geojson/farms", m.farmsGeoJSON)
	r.With(authorize("plot", "read")).Get("/geojson/plots", m.plotsGeoJSON)
	r.With(authorize("crop", "read")).Get("/disease-pests", m.listDiseasePests)
}
