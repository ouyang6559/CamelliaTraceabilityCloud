package remotesensing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/events"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Worker drives the job state machine: queued -> running -> succeeded/failed,
// publishing STAC items for produced assets and NATS events (specs/04.10:
// assets expressed as Catalog/Collection/Item/Asset).
type Worker struct {
	pool       *pgxpool.Pool
	processor  Processor
	processors map[string]Processor // optional per-type registry (gdal/nodeodm)
	pub        *events.Publisher
	log        *slog.Logger
}

func NewWorker(pool *pgxpool.Pool, processor Processor, pub *events.Publisher, log *slog.Logger) *Worker {
	return &Worker{pool: pool, processor: processor, pub: pub, log: log}
}

// RegisterProcessor adds a named processor selectable per job
// (job.processor field: mock | nodeodm | gdal).
func (w *Worker) RegisterProcessor(name string, p Processor) {
	if w.processors == nil {
		w.processors = map[string]Processor{}
	}
	w.processors[name] = p
}

// Start launches the background processing loop until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce advances queued -> running and running -> terminal states.
func (w *Worker) RunOnce(ctx context.Context) {
	w.promoteQueued(ctx)
	w.advanceRunning(ctx)
}

type jobRow struct {
	id        string
	tenantID  string
	jobType   string
	processor string
	extTaskID string
	inputs    []JobInput
}

func (w *Worker) promoteQueued(ctx context.Context) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, tenant_id, job_type, processor, inputs FROM remote_sensing_jobs
		WHERE status = 'queued' ORDER BY created_at LIMIT 5`)
	if err != nil {
		return
	}
	type queued struct {
		id, tenantID, jobType, processor string
		inputs                           []JobInput
	}
	var jobs []queued
	for rows.Next() {
		var q queued
		var inputsRaw []byte
		if err := rows.Scan(&q.id, &q.tenantID, &q.jobType, &q.processor, &inputsRaw); err != nil {
			break
		}
		if json.Unmarshal(inputsRaw, &q.inputs) != nil {
			q.inputs = []JobInput{}
		}
		jobs = append(jobs, q)
	}
	rows.Close()

	for _, q := range jobs {
		proc := w.processorFor(q.processor)
		if proc == nil {
			w.fail(ctx, q.id, q.tenantID, "no processor available for %q", q.processor)
			continue
		}
		extID, err := proc.Start(TenantContext(ctx, q.tenantID), q.jobType, q.inputs)
		if err != nil {
			w.fail(ctx, q.id, q.tenantID, "start failed: %v", err)
			continue
		}
		if _, err := w.pool.Exec(ctx, `
			UPDATE remote_sensing_jobs SET status='running', progress=5, started_at=now(),
				external_task_id=$3, updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND status='queued'`,
			q.tenantID, q.id, extID); err != nil {
			w.log.Warn("rs job start update failed", "job", q.id, "err", err)
			continue
		}
		w.pub.Publish(q.tenantID, "remote-sensing.updated", map[string]any{
			"job_id": q.id, "status": "running", "progress": 5,
		})
	}
}

func (w *Worker) advanceRunning(ctx context.Context) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, tenant_id, job_type, processor, external_task_id, inputs
		FROM remote_sensing_jobs WHERE status = 'running' ORDER BY created_at LIMIT 5`)
	if err != nil {
		return
	}
	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		var inputsRaw []byte
		if err := rows.Scan(&j.id, &j.tenantID, &j.jobType, &j.processor, &j.extTaskID, &inputsRaw); err != nil {
			break
		}
		if json.Unmarshal(inputsRaw, &j.inputs) != nil {
			j.inputs = []JobInput{}
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		proc := w.processorFor(j.processor)
		if proc == nil {
			w.fail(ctx, j.id, j.tenantID, "processor %q disappeared", j.processor)
			continue
		}
		state, err := proc.Poll(TenantContext(ctx, j.tenantID), j.jobType, j.extTaskID, j.inputs)
		if err != nil {
			w.fail(ctx, j.id, j.tenantID, "poll failed: %v", err)
			continue
		}
		if _, err := w.pool.Exec(ctx, `
			UPDATE remote_sensing_jobs SET progress=$3, updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND status='running'`,
			j.tenantID, j.id, state.Progress); err != nil {
			w.log.Warn("rs progress update failed", "job", j.id, "err", err)
			return
		}
		switch state.Status {
		case "running":
			w.pub.Publish(j.tenantID, "remote-sensing.updated", map[string]any{
				"job_id": j.id, "status": "running", "progress": state.Progress,
			})
		case "succeeded":
			w.succeed(ctx, j, state.Outputs)
		case "failed":
			w.fail(ctx, j.id, j.tenantID, "%s", state.Err)
		}
	}
}

// succeed marks the job succeeded, publishes a STAC item per output and emits
// the NATS event. STAC items follow OGC STAC Item structure (specs/04.10).
func (w *Worker) succeed(ctx context.Context, j jobRow, outputs []JobOutput) {
	outputsJSON, _ := json.Marshal(outputs)
	if _, err := w.pool.Exec(ctx, `
		UPDATE remote_sensing_jobs SET status='succeeded', progress=100, outputs=to_jsonb($3::jsonb),
			finished_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status='running'`,
		j.tenantID, j.id, string(outputsJSON)); err != nil {
		w.log.Warn("rs succeed update failed", "job", j.id, "err", err)
		return
	}

	// One STAC item per output asset, in the job-type collection
	// (orthophoto / ndvi / dem).
	for _, out := range outputs {
		itemID := ulidx.New()
		// STAC geometry: farm-scale jobs carry no precise footprint in the mock;
		// the item keeps a null geometry until real rasters provide footprints.
		item := map[string]any{
			"stac_version": "1.0.0",
			"type":         "Feature",
			"id":           itemID,
			"properties": map[string]any{
				"datetime":  time.Now().UTC().Format(time.RFC3339),
				"job_id":    j.id,
				"job_type":  j.jobType,
				"processor": w.processor.Name(),
			},
			"assets": map[string]any{
				out.Type: map[string]any{
					"href":  "/api/v1/files/" + out.FileID + "/content",
					"type":  "image/tiff; application=geotiff; profile=cloud-optimized",
					"title": out.Name,
				},
			},
			"links": []map[string]any{
				{"rel": "collection", "href": "/api/v1/stac/collections/" + j.jobType},
			},
		}
		itemJSON, _ := json.Marshal(item)
		if _, err := w.pool.Exec(ctx, `
			INSERT INTO stac_items (id, tenant_id, collection_id, item, mission_id, job_id, created_by)
			VALUES ($1,$2,$3,to_jsonb($4::jsonb),$5,$6,$7)
			ON CONFLICT (tenant_id, collection_id, id) DO NOTHING`,
			itemID, j.tenantID, j.jobType, string(itemJSON), nil, j.id, "rs-worker"); err != nil {
			w.log.Warn("stac item insert failed", "job", j.id, "err", err)
			continue
		}
		w.pub.Publish(j.tenantID, "remote-sensing.stac", map[string]any{
			"item_id": itemID, "collection": j.jobType, "job_id": j.id,
		})
	}

	w.pub.Publish(j.tenantID, "remote-sensing.updated", map[string]any{
		"job_id": j.id, "status": "succeeded", "progress": 100,
	})
	w.log.Info("rs job succeeded", "job", j.id, "outputs", len(outputs))
}

func (w *Worker) fail(ctx context.Context, jobID, tenantID, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if _, err := w.pool.Exec(ctx, `
		UPDATE remote_sensing_jobs SET status='failed', error=$3, finished_at=now(), updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND status IN ('queued','running')`,
		tenantID, jobID, msg); err != nil {
		w.log.Warn("rs fail update failed", "job", jobID, "err", err)
		return
	}
	w.pub.Publish(tenantID, "remote-sensing.updated", map[string]any{
		"job_id": jobID, "status": "failed", "error": msg,
	})
	w.log.Warn("rs job failed", "job", jobID, "err", msg)
}

// processorFor resolves the configured processor: per-job choice first
// (registry), falling back to the deployment default.
func (w *Worker) processorFor(name string) Processor {
	if p, ok := w.processors[name]; ok {
		return p
	}
	switch name {
	case "mock":
		return &MockProcessor{}
	case "nodeodm":
		if w.processor.Name() == "nodeodm" {
			return w.processor
		}
		return nil
	case "gdal":
		return nil // gdal registers itself only when gdal_translate is present
	default:
		return nil
	}
}

var _ = fmt.Sprintf

// buildStacItem constructs an OGC STAC 1.0.0 Item for one produced asset
// (specs/04.10: assets expressed as Catalog/Collection/Item/Asset).
func buildStacItem(itemID, collection, jobID, processorName string, out JobOutput, datetime time.Time) map[string]any {
	return map[string]any{
		"stac_version":    "1.0.0",
		"stac_extensions": []string{},
		"type":            "Feature",
		"id":              itemID,
		"collection":      collection,
		"properties": map[string]any{
			"datetime":  datetime.Format(time.RFC3339),
			"job_id":    jobID,
			"job_type":  collection,
			"processor": processorName,
		},
		"assets": map[string]any{
			out.Type: map[string]any{
				"href":  "/api/v1/files/" + out.FileID + "/content",
				"type":  "image/tiff; application=geotiff; profile=cloud-optimized",
				"title": out.Name,
				"roles": []string{collection},
			},
		},
		"links": []map[string]any{
			{"rel": "collection", "href": "/api/v1/stac/collections/" + collection},
			{"rel": "parent", "href": "/api/v1/stac/collections/" + collection},
		},
	}
}

// tenantKey carries the job tenant into processor sinks/fetchers (ADR-011).
type tenantKey struct{}

// TenantContext returns ctx carrying tenantID for processor file I/O.
func TenantContext(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFromContext extracts the job tenant set by TenantContext.
func TenantFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tenantKey{}).(string)
	return v
}
