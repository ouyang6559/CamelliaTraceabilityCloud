// Package seed loads the end-to-end demo dataset (specs/15.13 验收链路):
// tenant -> farm -> plots -> crop batch -> tasks -> devices -> harvest ->
// raw material -> processing -> product -> packages -> trace codes.
package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/identity"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/modules/trace"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/geo"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/passhash"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Run executes the demo seed; it is idempotent (skips when tenant exists).
func Run(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE code='demo')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		log.Info("demo tenant already present; seed skipped")
		return nil
	}

	s := &seeder{pool: pool, cfg: cfg, log: log, traceSvc: trace.NewService(pool, nil, log)}
	if err := s.run(ctx); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	log.Info("demo dataset loaded — login with admin / " + cfg.SeedAdminPassword + " (tenant code: demo)")
	return nil
}

type seeder struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	log      *slog.Logger
	traceSvc *trace.Service

	tenantID string
	adminID  string
	now      time.Time
}

func (s *seeder) run(ctx context.Context) error {
	s.now = time.Now().UTC()
	return s.tenant(ctx)
}

func (s *seeder) tenant(ctx context.Context) error {
	s.tenantID = ulidx.NewFromTime(s.now)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tenants (id, name, code, metadata, created_by)
		VALUES ($1,$2,'demo',to_jsonb($3::jsonb),'seed')`,
		s.tenantID, "湖南油茶示范园（演示）",
		mustJSON(map[string]any{"description": "CamelliaTraceabilityCloud demo tenant"})); err != nil {
		return err
	}
	s.log.Info("tenant created", "id", s.tenantID)
	return s.permissionsAndRoles(ctx)
}

func (s *seeder) permissionsAndRoles(ctx context.Context) error {
	for _, p := range identity.Permissions {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO permissions (id, code, resource, action, name)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (code) DO NOTHING`,
			"perm-"+p.Code, p.Code, p.Resource, p.Action, p.Name); err != nil {
			return err
		}
	}
	type roleDef struct {
		code, name, desc string
		perms            []string
	}
	roles := []roleDef{
		{"tenant_admin", "租户管理员", "Full management of the tenant", identity.AdminPerms},
		{"operator", "作业人员", "Field operations (tasks, devices, records)", identity.OperatorPerms},
		{"viewer", "只读用户", "Read-only access", identity.ViewerPerms},
	}
	roleIDs := map[string]string{}
	for _, r := range roles {
		roleID := ulidx.New()
		roleIDs[r.code] = roleID
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO roles (id, tenant_id, code, name, description, builtin, created_by)
			VALUES ($1,$2,$3,$4,$5,true,'seed')`,
			roleID, s.tenantID, r.code, r.name, r.desc); err != nil {
			return err
		}
		for _, code := range r.perms {
			if code == "*:*" {
				if _, err := s.pool.Exec(ctx, `
					INSERT INTO role_permissions (role_id, permission_id)
					SELECT $1, id FROM permissions ON CONFLICT DO NOTHING`, roleID); err != nil {
					return err
				}
				continue
			}
			if _, err := s.pool.Exec(ctx, `
				INSERT INTO role_permissions (role_id, permission_id)
				SELECT $1, id FROM permissions WHERE code=$2 ON CONFLICT DO NOTHING`, roleID, code); err != nil {
				return err
			}
		}
	}

	// Admin user (password from configuration; specs/08.7).
	hash, err := passhash.Hash(s.cfg.SeedAdminPassword)
	if err != nil {
		return err
	}
	s.adminID = ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, tenant_id, username, password_hash, display_name, email, locale, created_by)
		VALUES ($1,$2,'admin',$3,'演示管理员','admin@demo.local','zh-CN','seed')`,
		s.adminID, s.tenantID, hash); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2)`, s.adminID, roleIDs["tenant_admin"]); err != nil {
		return err
	}
	// A field operator account.
	operatorID := ulidx.New()
	opHash, _ := passhash.Hash(s.cfg.SeedAdminPassword)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, tenant_id, username, password_hash, display_name, locale, created_by)
		VALUES ($1,$2,'operator',$3,'张作业员','zh-CN','seed')`,
		operatorID, s.tenantID, opHash); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2)`, operatorID, roleIDs["operator"]); err != nil {
		return err
	}
	s.log.Info("roles & users ready", "admin", "admin", "operator", "operator")
	return s.farmAndPlots(ctx)
}

func (s *seeder) farmAndPlots(ctx context.Context) error {
	farmID := ulidx.New()
	boundary := `{"type":"Polygon","coordinates":[[[113.6150,28.1830],[113.6420,28.1830],[113.6420,28.2020],[113.6150,28.2020],[113.6150,28.1830]]]}`
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO farms (id, tenant_id, name, code, description, address, location, boundary, area_m2, created_by)
		VALUES ($1,$2,'浏阳油茶示范基地','FARM-001','油茶种植 + 加工一体化示范基地',
			'湖南省长沙市浏阳市沙市镇',
			ST_SetSRID(ST_MakePoint(113.6285, 28.1925),4326),
			ST_SetSRID(ST_GeomFromGeoJSON($3),4326), 171600, 'seed')`,
		farmID, s.tenantID, boundary); err != nil {
		return err
	}

	plots := []struct {
		name, code string
		poly       string
		area       float64
		soil       string
	}{
		{"北坡一号地块", "PLOT-001", `{"type":"Polygon","coordinates":[[[113.6150,28.1900],[113.6280,28.1900],[113.6280,28.2020],[113.6150,28.2020],[113.6150,28.1900]]]}`, 152000, "红壤"},
		{"南坡二号地块", "PLOT-002", `{"type":"Polygon","coordinates":[[[113.6280,28.1830],[113.6420,28.1830],[113.6420,28.1950],[113.6280,28.1950],[113.6280,28.1830]]]}`, 149000, "黄红壤"},
		{"溪谷育苗地块", "PLOT-003", `{"type":"Polygon","coordinates":[[[113.6280,28.1950],[113.6420,28.1950],[113.6420,28.2020],[113.6280,28.2020],[113.6280,28.1950]]]}`, 46000, "冲积土"},
	}
	plotIDs := map[string]string{}
	for _, p := range plots {
		plotID := ulidx.New()
		plotIDs[p.code] = plotID
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO plots (id, tenant_id, farm_id, name, code, boundary, area_m2, soil_type, source, created_by)
			VALUES ($1,$2,$3,$4,$5,ST_SetSRID(ST_GeomFromGeoJSON($6),4326),$7,$8,'manual','seed')`,
			plotID, s.tenantID, farmID, p.name, p.code, p.poly, p.area, p.soil); err != nil {
			return err
		}
	}

	// Crop + varieties.
	cropID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO crops (id, tenant_id, name, code, category, description, created_by)
		VALUES ($1,$2,'油茶','camellia-oleifera','oil-tea','普通油茶 Camellia oleifera Abel.','seed')`,
		cropID, s.tenantID); err != nil {
		return err
	}
	varieties := map[string]string{}
	for _, v := range []struct{ name, code, desc string }{
		{"湘林1号", "xianglin-1", "高产稳产良种，鲜果出籽率高"},
		{"长林40号", "changlin-40", "抗病性强，适配丘陵红壤"},
	} {
		vid := ulidx.New()
		varieties[v.code] = vid
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO varieties (id, tenant_id, crop_id, name, code, description, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,'seed')`, vid, s.tenantID, cropID, v.name, v.code, v.desc); err != nil {
			return err
		}
	}

	// Crop batch on plot 1.
	batchID := ulidx.New()
	planted := s.now.AddDate(-4, -2, 0)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO crop_batches (id, tenant_id, plot_id, crop_id, variety_id, name, code, planted_at, area_m2, status, created_by)
		VALUES ($1,$2,$3,$4,$5,'北坡一号油茶林批次','CB-2022-001',$6,152000,'growing','seed')`,
		batchID, s.tenantID, plotIDs["PLOT-001"], cropID, varieties["xianglin-1"], planted); err != nil {
		return err
	}

	// Dictionary entries.
	for _, dp := range []struct{ cat, zh, en, sci string }{
		{"disease", "油茶炭疽病", "Camellia anthracnose", "Colletotrichum gloeosporioides"},
		{"disease", "油茶软腐病", "Camellia soft rot", "Pestalotiopsis sp."},
		{"pest", "油茶象甲", "Camellia weevil", "Curculio chinensis"},
		{"pest", "茶梢蛾", "Tea shoot borer", "Prays endocarpa"},
	} {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO disease_pests (id, tenant_id, category, name_zh, name_en, scientific_name)
			VALUES ($1,NULL,$2,$3,$4,$5)`, ulidx.New(), dp.cat, dp.zh, dp.en, dp.sci); err != nil {
			return err
		}
	}
	s.log.Info("farm / plots / crops ready")
	return s.tasksAndInputs(ctx, batchID)
}

func (s *seeder) tasksAndInputs(ctx context.Context, batchID string) error {
	inputID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO inputs (id, tenant_id, name, code, category, unit, spec, created_by)
		VALUES ($1,$2,'有机生物肥（油茶专用）','input-yj-01','fertilizer','kg','N-P-K 18-8-14 有机质≥45%','seed')`,
		inputID, s.tenantID); err != nil {
		return err
	}
	inputBatchID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO input_batches (id, tenant_id, input_id, code, supplier, quantity, unit, produced_at, status, created_by)
		VALUES ($1,$2,$3,'IB-2026-001','湖南绿源生物科技',800,'kg',$4,'in_stock','seed')`,
		inputBatchID, s.tenantID, inputID, s.now.AddDate(0, -1, 0)); err != nil {
		return err
	}

	// Completed fertilizing task -> trace event.
	taskID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agricultural_tasks (id, tenant_id, type, title, description, status, priority,
			planned_start_at, started_at, completed_at, assignee_id, source, created_by)
		VALUES ($1,$2,'fertilize','春季施肥（有机肥 120kg）','沿树冠滴水线开沟施有机肥，覆土','completed','normal',
			$3,$4,$5,$6,'manual','seed')`,
		taskID, s.tenantID, s.now.AddDate(0, 0, -21), s.now.AddDate(0, 0, -21), s.now.AddDate(0, 0, -20), s.adminID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO input_usages (id, tenant_id, task_id, input_batch_id, quantity, unit, applied_by)
		VALUES ($1,$2,$3,$4,120,'kg',$5)`, ulidx.New(), s.tenantID, taskID, inputBatchID, s.adminID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE input_batches SET quantity = quantity - 120 WHERE id=$1`, inputBatchID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agricultural_task_records (id, tenant_id, task_id, operator_id, note, location, finished_at)
		VALUES ($1,$2,$3,$4,'沟施覆土完成，无明显流失','SRID=4326;POINT(113.6200 28.1950)',$5)`,
		ulidx.New(), s.tenantID, taskID, s.adminID, s.now.AddDate(0, 0, -20)); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "fertilize", Title: "春季施肥（有机肥 120kg）",
		Description: "沿树冠滴水线开沟施有机肥", CropBatchID: batchID,
		RefType: "task", RefID: taskID, TaskID: taskID, OperatorID: s.adminID,
		Location: &geo.Point{Lat: 28.1950, Lng: 113.6200},
		Quantity: fptr(120), Unit: "kg",
		Inputs:   []any{map[string]any{"input_batch": "IB-2026-001", "quantity": 120, "unit": "kg"}},
		Metadata: map[string]any{"source": "seed"},
	}); err != nil {
		return err
	}

	// Planned pruning task.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agricultural_tasks (id, tenant_id, type, title, description, status, priority,
			planned_start_at, planned_end_at, assignee_id, created_by)
		VALUES ($1,$2,'prune','冬季修剪','疏除过密枝、下垂枝，改善通风透光','planned','normal',
			$3,$4,$5,'seed')`,
		ulidx.New(), s.tenantID, s.now.AddDate(0, 0, 3), s.now.AddDate(0, 0, 7), s.adminID); err != nil {
		return err
	}

	// In-progress inspection task.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO agricultural_tasks (id, tenant_id, type, title, description, status, priority,
			planned_start_at, started_at, assignee_id, created_by)
		VALUES ($1,$2,'inspect','巡田检查炭疽病','重点关注果实病斑与落果','in_progress','high',
			$3,$4,$5,'seed')`,
		ulidx.New(), s.tenantID, s.now.AddDate(0, 0, -1), s.now.AddDate(0, 0, -1), s.adminID); err != nil {
		return err
	}
	s.log.Info("tasks / inputs ready")
	return s.devices(ctx, batchID)
}

func (s *seeder) devices(ctx context.Context, batchID string) error {
	type deviceDef struct {
		name, code, dtype string
		caps              []string
		lat, lng          float64
	}
	defs := []deviceDef{
		{"气象站-01", "DEV-WS-001", "WEATHER_STATION", []string{"TELEMETRY"}, 28.1925, 113.6285},
		{"土壤传感器-01", "DEV-SOIL-001", "SOIL_SENSOR", []string{"TELEMETRY"}, 28.1950, 113.6200},
		{"植保无人机-01", "DEV-UAV-001", "UAV", []string{"TELEMETRY", "GPS", "MISSION", "SPRAY", "MAPPING", "PHOTO", "INSPECTION"}, 28.1925, 113.6285},
		{"割草机器人-01", "DEV-MOW-001", "MOWER", []string{"TELEMETRY", "GPS", "MISSION", "MOW", "AUTONOMOUS"}, 28.1900, 113.6250},
	}
	tokens := map[string]string{}
	deviceIDs := map[string]string{}
	for _, d := range defs {
		deviceID := ulidx.New()
		deviceIDs[d.code] = deviceID
		token := "ctc_" + ulidx.New()
		tokens[d.code] = token
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO devices (id, tenant_id, name, code, device_type, status, capabilities,
				token_hash, location, battery_level, firmware, adapter_type, created_by)
			VALUES ($1,$2,$3,$4,$5,'offline',$6,$7,
				ST_SetSRID(ST_MakePoint($8,$9),4326), 95,'v1.0.0','generic-mqtt','seed')`,
			deviceID, s.tenantID, d.name, d.code, d.dtype, d.caps,
			hashToken(token), d.lng, d.lat); err != nil {
			return err
		}
	}

	// Alert rule: greenhouse-level temperature threshold.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO alert_rules (id, tenant_id, name, device_type, metric, operator, threshold, severity, enabled, created_by)
		VALUES ($1,$2,'高温告警','WEATHER_STATION','temperature','>',40,'warning',true,'seed')`,
		ulidx.New(), s.tenantID); err != nil {
		return err
	}
	// Alert rule for soil moisture.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO alert_rules (id, tenant_id, name, device_type, metric, operator, threshold, severity, enabled, created_by)
		VALUES ($1,$2,'土壤干旱告警','SOIL_SENSOR','soil_moisture','<',18,'critical',true,'seed')`,
		ulidx.New(), s.tenantID); err != nil {
		return err
	}

	// Sample drone mission (draft -> demo the safety pipeline in UI).
	missionID := ulidx.New()
	route := []map[string]any{
		{"lat": 28.1905, "lng": 113.6160, "alt": 50, "action": "takeoff"},
		{"lat": 28.1930, "lng": 113.6200, "alt": 60, "action": "wayline", "speed": 4},
		{"lat": 28.1980, "lng": 113.6240, "alt": 60, "action": "spray_on"},
		{"lat": 28.2010, "lng": 113.6270, "alt": 55, "action": "photo"},
		{"lat": 28.1960, "lng": 113.6275, "alt": 50, "action": "land"},
	}
	routeJSON, _ := json.Marshal(route)
	line := "LINESTRING(113.616000 28.190500, 113.620000 28.193000, 113.624000 28.198000, 113.627000 28.201000, 113.627500 28.196000)"
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO missions (id, tenant_id, device_id, name, mission_type, status, risk_level,
			route, route_geometry, planned_start_at, created_by)
		VALUES ($1,$2,$3,'北坡巡检+喷洒演示','spray','draft','high',
			to_jsonb($4::jsonb), ST_SetSRID(ST_GeomFromText($5),4326), $6,'seed')`,
		missionID, s.tenantID, deviceIDs["DEV-UAV-001"], string(routeJSON), line, s.now.AddDate(0, 0, 1)); err != nil {
		return err
	}

	// Persist device credentials for the simulator (data/ is gitignored).
	if err := os.MkdirAll("data", 0o755); err == nil {
		devices := map[string]any{}
		for code, token := range tokens {
			devices[code] = map[string]any{"id": deviceIDs[code], "token": token}
		}
		payload, _ := json.MarshalIndent(map[string]any{
			"tenant_id": s.tenantID,
			"devices":   devices,
			"note":      "Simulator credentials generated by seed; do not commit.",
		}, "", "  ")
		_ = os.WriteFile(filepath.Join("data", "seed-devices.json"), payload, 0o600)
	}
	s.log.Info("devices / alert rules / demo mission ready",
		"device_tokens", "data/seed-devices.json")
	return s.chain(ctx, batchID, deviceIDs)
}

func (s *seeder) chain(ctx context.Context, batchID string, deviceIDs map[string]string) error {
	// Harvest.
	harvestID := ulidx.New()
	harvestedAt := s.now.AddDate(0, 0, -10)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO harvests (id, tenant_id, plot_id, crop_batch_id, harvested_at, quantity, unit, operator_id, note, status, created_by)
		VALUES ($1,$2,(SELECT plot_id FROM crop_batches WHERE id=$3),$3,$4,2300,'kg',$5,'霜降后油茶果充分成熟，人工+机械采收','completed','seed')`,
		harvestID, s.tenantID, batchID, harvestedAt, s.adminID); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "harvest", Title: "油茶果采收 2300kg", CropBatchID: batchID,
		RefType: "harvest", RefID: harvestID, OperatorID: s.adminID,
		Quantity: fptr(2300), Unit: "kg",
		Metadata: map[string]any{"method": "manual+machine"},
	}); err != nil {
		return err
	}

	// Raw material batch.
	rmID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO raw_material_batches (id, tenant_id, code, harvest_id, crop_batch_id, quantity, unit, received_at, status, created_by)
		VALUES ($1,$2,'RM-2026-001',$3,$4,2300,'kg',$5,'consumed','seed')`,
		rmID, s.tenantID, harvestID, batchID, harvestedAt.Add(4*time.Hour)); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "warehouse_in", Title: "原料批次 RM-2026-001 入库",
		RefType: "raw_material_batch", RefID: rmID, CropBatchID: batchID,
		OperatorID: s.adminID, Quantity: fptr(2300), Unit: "kg",
	}); err != nil {
		return err
	}

	// Processing batch.
	prID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO processing_batches (id, tenant_id, code, raw_material_batch_id, process_type,
			input_quantity, output_quantity, unit, operator_id, started_at, finished_at, parameters, status, created_by)
		VALUES ($1,$2,'PR-2026-001',$3,'pressing',2300,690,'kg',$4,$5,$6,
			to_jsonb($7::jsonb),'completed','seed')`,
		prID, s.tenantID, rmID, s.adminID,
		harvestedAt.AddDate(0, 0, 1), harvestedAt.AddDate(0, 0, 4),
		mustJSON(map[string]any{"工艺": "低温物理压榨", "温度": "≤60℃"})); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "processing", Title: "低温压榨 2300kg -> 690kg 毛油",
		RefType: "processing_batch", RefID: prID, CropBatchID: batchID,
		OperatorID: s.adminID, Quantity: fptr(690), Unit: "kg",
		Weather: map[string]any{"note": "车间恒温"},
	}); err != nil {
		return err
	}

	// Product + product batch.
	productID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO products (id, tenant_id, name, code, category, spec, unit, description, created_by)
		VALUES ($1,$2,'山茶油 750ml 玻璃瓶装','product-cyo-750','camellia-oil','750ml/瓶','bottle','一级冷榨山茶油','seed')`,
		productID, s.tenantID); err != nil {
		return err
	}
	pbID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO product_batches (id, tenant_id, code, product_id, processing_batch_id, produced_at, quantity, unit, status, created_by)
		VALUES ($1,$2,'PB-2026-001',$3,$4,$5,900,'bottle','available','seed')`,
		pbID, s.tenantID, productID, prID, harvestedAt.AddDate(0, 0, 6)); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "packing", Title: "产品批次 PB-2026-001 灌装 900 瓶",
		RefType: "product_batch", RefID: pbID, CropBatchID: batchID,
		OperatorID: s.adminID, Quantity: fptr(900), Unit: "bottle",
	}); err != nil {
		return err
	}

	// Quality test (acid value).
	testID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO quality_tests (id, tenant_id, ref_type, ref_id, item, standard, method,
			result_value, result_text, unit, passed, tested_by, tested_at, note)
		VALUES ($1,$2,'product_batch',$3,'acid_value','GB/T 11765-2018 一级','GB 5009.229',0.68,'','mg KOH/g',true,$4,$5,'合格')`,
		testID, s.tenantID, pbID, s.adminID, harvestedAt.AddDate(0, 0, 7)); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "quality_test", Title: "酸价检测 0.68 mg KOH/g（合格）",
		RefType: "product_batch", RefID: pbID, CropBatchID: batchID,
		OperatorID: s.adminID,
		Metadata:   map[string]any{"item": "acid_value", "passed": true, "standard": "GB/T 11765-2018"},
	}); err != nil {
		return err
	}

	// Packages + trace codes.
	pkgIDs := []string{}
	for i := 1; i <= 3; i++ {
		pkgID := ulidx.New()
		pkgIDs = append(pkgIDs, pkgID)
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO packages (id, tenant_id, code, product_batch_id, quantity, unit, packed_at, net_weight, status, created_by)
			VALUES ($1,$2,$3,$4,1,'bottle',$5,0.75,'stocked','seed')`,
			pkgID, s.tenantID, fmt.Sprintf("PK-2026-%03d", i), pbID, harvestedAt.AddDate(0, 0, 8)); err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO trace_codes (id, tenant_id, code, code_type, ref_type, ref_id, product_batch_id, package_id, created_by)
			VALUES ($1,$2,$3,'consumer','package',$4,$5,$6,'seed')`,
			ulidx.New(), s.tenantID, "CTC-U-"+fmt.Sprintf("%08d", 20260000+i), pkgID, pbID, pkgID); err != nil {
			return err
		}
	}

	// External platform (mock).
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO external_platforms (id, tenant_id, name, provider_type, status, capabilities, config, created_by)
		VALUES ($1,$2,'演示溯源平台（Mock）','mock','CONNECTED',$3,to_jsonb('{}'::jsonb),'seed')`,
		ulidx.New(), s.tenantID,
		[]string{"BATCH", "TRACE_EVENT", "QUERY", "VERIFY"}); err != nil {
		return err
	}

	// Warehouse + inventory in.
	warehouseID := ulidx.New()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO warehouses (id, tenant_id, name, code, address, location, capacity, unit, created_by)
		VALUES ($1,$2,'成品一号仓','WH-001','浏阳市沙市镇示范园内',
			ST_SetSRID(ST_MakePoint(113.6290,28.1930),4326), 5000,'bottle','seed')`,
		warehouseID, s.tenantID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO inventory_items (id, tenant_id, warehouse_id, ref_type, ref_id, quantity, unit)
		VALUES ($1,$2,$3,'product_batch',$4,900,'bottle')`, ulidx.New(), s.tenantID, warehouseID, pbID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO inventory_movements (id, tenant_id, warehouse_id, ref_type, ref_id, direction, quantity, unit, reason, operator_id)
		VALUES ($1,$2,$3,'product_batch',$4,'in',900,'bottle','PB-2026-001 灌装入库',$5)`,
		ulidx.New(), s.tenantID, warehouseID, pbID, s.adminID); err != nil {
		return err
	}
	if _, err := s.traceSvc.Append(ctx, s.tenantID, trace.EventInput{
		EventType: "warehouse_in", Title: "成品 900 瓶入成品一号仓",
		RefType: "product_batch", RefID: pbID, CropBatchID: batchID,
		OperatorID: s.adminID, Quantity: fptr(900), Unit: "bottle",
	}); err != nil {
		return err
	}

	s.log.Info("production / trace chain ready", "packages", len(pkgIDs),
		"qr_prefix", s.cfg.TraceCodeURLPrefix)
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func fptr(f float64) *float64 { return &f }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
