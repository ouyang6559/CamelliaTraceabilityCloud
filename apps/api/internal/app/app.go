// Package app wires configuration, infrastructure and all domain modules into
// the runnable HTTP server (modular monolith, specs/01).
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	djiAdapter "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device/dji"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device/genericmqtt"
	mavlinkAdapter "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device/mavlink"
	rosbridgeAdapter "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/device/rosbridge"
	traceadapter "github.com/ouyang6559/CamelliaTraceabilityCloud/adapters/trace/mock"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/middleware"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/agriculture"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/ai"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/alert"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/audit"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/dashboard"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/device"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/evidence"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/files"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/identity"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/iot"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/mission"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/production"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/quality"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/remotesensing"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/stream"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/sync"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/platform"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/seed"
)

// App is the assembled server.
type App struct {
	Cfg        *config.Config
	Log        *slog.Logger
	Pool       *pgxpool.Pool
	Redis      *redis.Client
	NATSConn   *nats.Conn
	Storage    platform.ObjectStorage
	syncModule *sync.Module
	mqttClient *platform.MQTTClient
	handler    http.Handler
}

// New assembles every dependency. It fails fast when core infrastructure is unavailable.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	pool, err := platform.NewDB(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	log.Info("database ready")

	rdb, err := platform.NewRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	log.Info("redis ready")

	natsConn, err := platform.NewNATS(ctx, cfg, log)
	if err != nil {
		// The event bus degrades gracefully (Local First, specs/01.7).
		log.Warn("nats unavailable; domain events disabled", "err", err)
	} else {
		log.Info("nats ready")
	}

	storage, err := platform.NewStorage(ctx, cfg, log)
	if err != nil {
		pool.Close()
		rdb.Close()
		return nil, err
	}

	jwt := middleware.NewJWT(cfg.JWTSecret, cfg.AccessTokenTTL)
	pub := platform.EventsPublisher(natsConn, log)

	// SaaS multi-tenancy bootstrap (docs/adr/adr-006): permission catalog +
	// platform administrator account.
	if err := identity.EnsurePermissionCatalog(ctx, pool); err != nil {
		return nil, fmt.Errorf("ensure permission catalog: %w", err)
	}
	if err := identity.EnsurePlatformAdmin(ctx, pool, cfg, log); err != nil {
		return nil, fmt.Errorf("ensure platform admin: %w", err)
	}
	if cfg.PlatformAdminPassword == "platform123" {
		log.Warn("CTC_PLATFORM_ADMIN_PASSWORD not set; using development default — do not use in production")
	}

	app := &App{
		Cfg: cfg, Log: log, Pool: pool, Redis: rdb, NATSConn: natsConn, Storage: storage,
	}

	// ---- domain services ----
	traceSvc := trace.NewService(pool, pub, log)
	devModule := device.NewModule(pool, pub, log)
	alertModule := alert.NewModule(pool, pub, log)
	devModule.SetAlertEvaluator(alertModule.EvaluateTelemetry)

	// Weather provider (docs/adr/adr-010): openmeteo (public API) or mock,
	// wrapped with an in-memory TTL cache.
	var weatherProvider iot.WeatherProvider
	switch cfg.WeatherProvider {
	case "mock":
		weatherProvider = &iot.MockProvider{}
	default:
		weatherProvider = &iot.OpenMeteoProvider{}
	}
	weatherProvider = iot.NewCachedProvider(weatherProvider, 15*time.Minute)
	log.Info("weather provider", "provider", weatherProvider.Name())

	// DeviceAdapter registry (specs/20, ADR-007): adapters registered by
	// adapter_type. MAVLink targets ArduPilot/PX4 vehicles (Endpoint =
	// udp:ip:port in device metadata); rosbridge targets ROS 2 robots
	// (Endpoint = ws://host:9090). dji-cloud stays unregistered until official
	// credentials exist (specs/03.6 — do not fabricate APIs).
	deviceRegistry := device.NewRegistry(pool, log)
	if mqttAdapter, err := genericmqtt.New(cfg.MQTTURL, log); err == nil {
		deviceRegistry.Register("generic-mqtt", mqttAdapter)
		log.Info("device adapter registered", "type", "generic-mqtt")
	} else {
		log.Warn("generic-mqtt adapter unavailable; device dispatch disabled", "err", err)
	}
	deviceRegistry.Register("mavlink", mavlinkAdapter.New(log))
	log.Info("device adapter registered", "type", "mavlink")
	// dji-cloud skeleton: OSD telemetry loopback-tested; mission ops fail
	// honestly until enterprise credentials + hardware validation (specs/03.6).
	deviceRegistry.Register("dji-cloud", djiAdapter.New(log))
	log.Info("device adapter registered", "type", "dji-cloud")
	deviceRegistry.Register("rosbridge", rosbridgeAdapter.New(log))
	log.Info("device adapter registered", "type", "rosbridge")
	if cfg.DJIAppKey != "" {
		log.Warn("DJI credentials present but the dji-cloud adapter is not implemented yet (integration TODO, specs/03.6)")
	}

	missionModule := mission.NewModule(pool, traceSvc, pub)
	missionModule.SetDeviceRecorder(devModule)
	missionModule.SetDeviceDispatcher(deviceRegistry)

	traceAPI := trace.NewAPI(traceSvc)
	filesModule := files.NewModule(pool, storage, cfg.S3Bucket, middleware.Auth(jwt))

	// ---- HTTP assembly ----
	authorize := middleware.RequirePerm
	auth := middleware.Auth(jwt)
	auditMW := middleware.Audit(pool, log)
	rateLimit := middleware.RateLimit(rdb, 600, time.Minute)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logging(log))
	r.Use(middleware.CORS([]string{"*"}))
	r.Use(rateLimit)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/v1", func(api chi.Router) {
		// Public consumer endpoints (no auth, specs/11.5).
		traceAPI.MountPublic(api)
		traceAPI.MountPublicMedia(api, filesModule.ServeEvidenceFile)
		devModule.MountDeviceGateway(api) // device-token authenticated (no user JWT)

		// Identity mounts public /auth (incl. self-registration) + platform
		// console routes and protected /users, /roles, /tenants.
		identityModule := identity.NewModule(pool, jwt)
		identityModule.SetContext(cfg, log)
		identityModule.Mount(api)

		// Private API: JWT + audit middleware for everything below.
		api.Group(func(priv chi.Router) {
			priv.Use(auth)
			priv.Use(auditMW)

			agri := agriculture.NewModule(pool, traceSvc, pub)
			agri.Mount(priv, authorize)

			prod := production.NewModule(pool, traceSvc, pub)
			prod.Mount(priv, authorize)

			traceAPI.Mount(priv)

			evModule := evidence.NewModule(pool, storage, nil, pub)
			evModule.SetPermMiddleware(authorize("evidence", "read"), authorize("evidence", "create"))
			evModule.Mount(priv)

			devModule.SetRegistry(deviceRegistry)
			devModule.Mount(priv, auth, authorize)
			missionModule.Mount(priv, authorize)
			missionModule.MountDeviceGateway(api) // device-token authenticated
			iotModule := iot.NewModule(pool)
			iotModule.SetWeatherProvider(weatherProvider)
			iotModule.Mount(priv, authorize)

			alertModule.Mount(priv, authorize)
			quality.NewModule(pool, traceSvc).Mount(priv, authorize)

			// Phase 4 remote sensing (ADR-008/ADR-011): processor per job —
			// mock (default), nodeodm (when configured), gdal (when the
			// gdal_translate binary is available in this image).
			var rsProcessor remotesensing.Processor = &remotesensing.MockProcessor{}
			if cfg.NodeODMURL != "" {
				rsProcessor = &remotesensing.NodeODMProcessor{
					BaseURL: cfg.NodeODMURL,
					Token:   cfg.NodeODMToken,
				}
				log.Info("nodeodm processor configured", "url", cfg.NodeODMURL)
			}
			rsWorker := remotesensing.NewWorker(pool, rsProcessor, pub, log)

			// Tenant-scoped file sink + input fetcher for the GDAL processor.
			rsSink := remotesensingFileSink{pool: pool, storage: storage}
			rsFetcher := remotesensingInputFetcher{pool: pool, storage: storage}
			if gdalProc, err := remotesensing.NewGDALProcessor(rsSink, rsFetcher, "", log); err == nil {
				rsWorker.RegisterProcessor("gdal", gdalProc)
				log.Info("gdal processor available (real COG conversion)", "bin", "gdal_translate")
			} else {
				log.Info("gdal processor unavailable; gdal jobs will fail honestly", "reason", err.Error())
			}
			rsWorker.Start(ctx)
			remotesensing.NewAPI(pool).Mount(priv, authorize)

			providers := sync.ProviderRegistry{
				"mock": &traceadapter.Provider{},
			}
			syncModule := sync.NewModule(pool, providers, pub, log, cfg.SyncInterval)
			syncModule.Mount(priv, authorize)
			app.syncModule = syncModule

			aiModule := ai.NewModule(pool, cfg, log)
			aiModule.SetWeatherProvider(weatherProvider)
			switch cfg.AIEmbeddings {
			case "off":
				log.Info("ai embeddings disabled; keyword retrieval only")
			case "openai":
				aiModule.SetEmbedder(&ai.OpenAIEmbedder{
					BaseURL: cfg.AIBaseURL,
					APIKey:  cfg.AIAPIKey,
					Model:   cfg.AIEmbeddingModel,
				})
				log.Info("ai embeddings", "provider", "openai-compatible", "model", cfg.AIEmbeddingModel)
			default:
				aiModule.SetEmbedder(&ai.MockEmbedder{})
				log.Info("ai embeddings", "provider", "mock")
			}
			aiModule.Mount(priv, authorize)

			filesModule.Mount(priv, authorize)

			audit.NewModule(pool).Mount(priv, authorize)
			dashboard.NewModule(pool).Mount(priv, authorize)
			stream.NewModule(natsConn, log, auth).Mount(priv)
		})
	})
	app.handler = r

	// ---- MQTT subscriber (telemetry ingest, specs/09.11) ----
	mqttClient, err := platform.ConnectMQTT(ctx, cfg.MQTTURL, "ctc-api-subscriber",
		[]string{"ctc/v1/+/devices/+/telemetry", "ctc/v1/+/devices/+/status"},
		devModule.MQTTHandler, log)
	if err != nil {
		log.Warn("mqtt unavailable at startup; paho keeps retrying in background", "err", err)
	} else {
		app.mqttClient = mqttClient
		log.Info("mqtt ready")
	}
	return app, nil
}

// remotesensingFileSink stores produced rasters as tenant files (ADR-011).
type remotesensingFileSink struct {
	pool    *pgxpool.Pool
	storage platform.ObjectStorage
}

func (s remotesensingFileSink) SaveOutput(ctx context.Context, filename, contentType string, r io.Reader) (string, error) {
	// The GDAL worker resolves the job tenant per input; the sink needs the
	// tenant too — the worker-owned fetcher/sink closures are created per
	// module here, so we thread tenant via the filename prefix is NOT tenable.
	// Instead the sink accepts the tenant from a context value set by the
	// worker's job-scoped goroutine.
	tenantID := remotesensing.TenantFromContext(ctx)
	if tenantID == "" {
		return "", fmt.Errorf("tenant not resolved for output sink")
	}
	objectKey := tenantID + "/rs/" + filename
	sha, err := platform.PutFile(ctx, s.storage, objectKey, r)
	if err != nil {
		return "", err
	}
	fileID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO files (id, tenant_id, bucket, object_key, filename, mime, size, sha256, uploaded_by)
		VALUES ($1,$2,'rs',$3,$4,$5,0,$6,'rs-worker')`,
		fileID, tenantID, objectKey, filename, contentType, sha); err != nil {
		return "", err
	}
	return fileID, nil
}

// remotesensingInputFetcher fetches job input files (tenant-scoped via context).
type remotesensingInputFetcher struct {
	pool    *pgxpool.Pool
	storage platform.ObjectStorage
}

func (f remotesensingInputFetcher) FetchInput(ctx context.Context, fileID string) (io.ReadCloser, string, error) {
	tenantID := remotesensing.TenantFromContext(ctx)
	var objectKey, filename string
	err := f.pool.QueryRow(ctx,
		`SELECT object_key, filename FROM files WHERE tenant_id=$1 AND id=$2`,
		tenantID, fileID).Scan(&objectKey, &filename)
	if err != nil {
		return nil, "", err
	}
	rc, err := f.storage.Open(ctx, objectKey)
	if err != nil {
		return nil, "", err
	}
	return rc, filename, nil
}

// Handler returns the assembled HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// StartSyncWorker launches the background external sync worker (Local First).
func (a *App) StartSyncWorker(ctx context.Context) {
	if a.syncModule != nil {
		a.syncModule.Start(ctx)
	}
}

// RunMigrate applies migrations only.
func RunMigrate(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	pool, err := platform.NewDB(ctx, cfg, log)
	if err != nil {
		return err
	}
	pool.Close()
	return nil
}

// RunSeed applies migrations (via New) and loads the demo dataset.
func RunSeed(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	a, err := New(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.Close()
	return seed.Run(ctx, a.Pool, cfg, log)
}

// Close releases connections.
func (a *App) Close() {
	if a.mqttClient != nil {
		a.mqttClient.Close()
	}
	a.Pool.Close()
	a.Redis.Close()
	if a.NATSConn != nil && !a.NATSConn.IsClosed() {
		_ = a.NATSConn.Drain()
	}
}
