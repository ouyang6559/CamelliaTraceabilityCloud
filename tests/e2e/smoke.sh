#!/usr/bin/env bash
# CamelliaTraceabilityCloud end-to-end smoke test (specs/17 MVP acceptance).
# Covers: auth -> farm/plot -> crop batch -> task -> harvest -> raw material ->
# processing -> product batch -> package -> trace code -> public trace ->
# mission safety pipeline -> telemetry -> alerts -> blockchain proof ->
# trace chain verification -> external sync -> AI chat -> audit.
#
# Usage: BASE_URL=http://localhost:8080 bash tests/e2e/smoke.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
API="$BASE_URL/api/v1"
TAG="S$(date +%s | tail -c 6)"   # unique-per-run suffix keeps the script re-runnable
PASS=0

step() { printf '\n\033[36m▶ %s\033[0m\n' "$1"; }
ok()   { PASS=$((PASS+1)); printf '  \033[32m✔\033[0m %s\n' "$1"; }

jqget() { python3 -c "import sys, json; d=json.load(sys.stdin); print(eval(\"d$1\"))" 2>/dev/null; }

step "health"
curl -sf "$BASE_URL/healthz" | grep -q '"ok"' && ok "healthz"

step "auth: login admin/admin123"
LOGIN=$(curl -sf -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123","tenant_code":"demo"}')
TOKEN=$(echo "$LOGIN" | jqget "['tokens']['access_token']")
[ -n "$TOKEN" ] && ok "access token acquired"
AUTH="Authorization: Bearer $TOKEN"

step "auth: /me returns tenant"
ME=$(curl -sf "$API/auth/me" -H "$AUTH")
echo "$ME" | grep -q '"code":"demo"' && ok "tenant demo"

step "agriculture: create farm"
FARM=$(curl -sf -X POST "$API/farms" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"name\":\"冒烟测试基地\",\"code\":\"SMOKE-FARM-$TAG\",\"address\":\"测试地址\",
  \"boundary\":{\"type\":\"Polygon\",\"coordinates\":[[[113.60,28.18],[113.61,28.18],[113.61,28.19],[113.60,28.19],[113.60,28.18]]]}
}")
FARM_ID=$(echo "$FARM" | jqget "['id']")
[ -n "$FARM_ID" ] && ok "farm $FARM_ID"

step "agriculture: create plot with boundary"
PLOT=$(curl -sf -X POST "$API/plots" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"farm_id\":\"$FARM_ID\",\"name\":\"冒烟地块\",\"code\":\"SMOKE-PLOT-$TAG\",
  \"boundary\":{\"type\":\"Polygon\",\"coordinates\":[[[113.602,28.181],[113.609,28.181],[113.609,28.188],[113.602,28.188],[113.602,28.181]]]},
  \"area_m2\":6000
}")
PLOT_ID=$(echo "$PLOT" | jqget "['id']")
[ -n "$PLOT_ID" ] && ok "plot $PLOT_ID"

step "agriculture: plot geojson contains geometry"
curl -sf "$API/geojson/plots" -H "$AUTH" | grep -q 'FeatureCollection' && ok "geojson"

step "agriculture: create crop + crop batch"
CROP=$(curl -sf -X POST "$API/crops" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"油茶(冒烟)\",\"code\":\"smoke-crop-$TAG\",\"category\":\"oil-tea\"}")
CROP_ID=$(echo "$CROP" | jqget "['id']")
BATCH=$(curl -sf -X POST "$API/crop-batches" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"plot_id\":\"$PLOT_ID\",\"crop_id\":\"$CROP_ID\",\"name\":\"冒烟批次\",\"code\":\"SMOKE-CB-$TAG\",\"planted_at\":\"2024-03-01T00:00:00Z\"}")
BATCH_ID=$(echo "$BATCH" | jqget "['id']")
[ -n "$BATCH_ID" ] && ok "crop batch $BATCH_ID"

step "agriculture: task lifecycle -> trace event"
TASK=$(curl -sf -X POST "$API/agricultural-tasks" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"title\":\"冒烟施肥任务\",\"type\":\"fertilize\",\"plot_id\":\"$PLOT_ID\",\"crop_batch_id\":\"$BATCH_ID\"}")
TASK_ID=$(echo "$TASK" | jqget "['id']")
curl -sf -X POST "$API/agricultural-tasks/$TASK_ID/start" -H "$AUTH" > /dev/null
COMPLETE=$(curl -sf -X POST "$API/agricultural-tasks/$TASK_ID/complete" -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"note":"冒烟完成","latitude":28.18,"longitude":113.60}')
echo "$COMPLETE" | grep -q 'record_id' && ok "task completed + trace event"

step "production: harvest -> raw material batch"
HARVEST=$(curl -sf -X POST "$API/harvests" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"plot_id\":\"$PLOT_ID\",\"crop_batch_id\":\"$BATCH_ID\",\"quantity\":100,\"unit\":\"kg\",\"harvested_at\":\"2026-11-01T00:00:00Z\"}")
HARVEST_ID=$(echo "$HARVEST" | jqget "['id']")
ACCEPT=$(curl -sf -X POST "$API/harvests/$HARVEST_ID/accept" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"code\":\"SMOKE-RM-$TAG\"}")
RM_ID=$(echo "$ACCEPT" | jqget "['id']")
[ -n "$RM_ID" ] && ok "raw material $RM_ID"

step "production: processing batch -> complete"
PR=$(curl -sf -X POST "$API/processing-batches" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"code\":\"SMOKE-PR-$TAG\",\"raw_material_batch_id\":\"$RM_ID\",\"process_type\":\"pressing\",\"input_quantity\":100,\"unit\":\"kg\"}")
PR_ID=$(echo "$PR" | jqget "['id']")
curl -sf -X POST "$API/processing-batches/$PR_ID/complete" -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"output_quantity":30}' | grep -q '"status":"completed"' && ok "processing completed"

step "production: product + product batch + package"
PRODUCT=$(curl -sf -X POST "$API/products" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"冒烟山茶油\",\"code\":\"smoke-oil-$TAG\",\"category\":\"camellia-oil\",\"unit\":\"bottle\"}")
PRODUCT_ID=$(echo "$PRODUCT" | jqget "['id']")
PB=$(curl -sf -X POST "$API/product-batches" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"code\":\"SMOKE-PB-$TAG\",\"product_id\":\"$PRODUCT_ID\",\"processing_batch_id\":\"$PR_ID\",\"quantity\":30,\"unit\":\"bottle\"}")
PB_ID=$(echo "$PB" | jqget "['id']")
PKG=$(curl -sf -X POST "$API/packages" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"code\":\"SMOKE-PK-$TAG\",\"product_batch_id\":\"$PB_ID\",\"quantity\":1,\"unit\":\"bottle\"}")
PKG_ID=$(echo "$PKG" | jqget "['id']")
[ -n "$PKG_ID" ] && ok "package $PKG_ID"

step "trace: generate consumer code + public trace endpoint"
CODE_RES=$(curl -sf -X POST "$API/trace-codes" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"ref_type\":\"package\",\"ref_id\":\"$PKG_ID\",\"code_type\":\"consumer\",\"product_batch_id\":\"$PB_ID\"}")
TRACE_CODE=$(echo "$CODE_RES" | jqget "['items'][0]['code']")
[ -n "$TRACE_CODE" ] && ok "trace code $TRACE_CODE"
PUBLIC=$(curl -sf "$API/public/trace/$TRACE_CODE")
echo "$PUBLIC" | grep -q 'timeline' && ok "public trace served"
echo "$PUBLIC" | grep -q '冒烟' && ok "chain summary reaches product"

step "trace: hash chain verification"
curl -sf -X POST "$API/trace-events/verify-chain" -H "$AUTH" | grep -q '"valid":true' && ok "hash chain valid"

step "device: register drone + telemetry via device gateway"
DEV=$(curl -sf -X POST "$API/devices" -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "name":"冒烟无人机","device_type":"UAV","capabilities":["TELEMETRY","GPS","MISSION","SPRAY"]}')
DEV_ID=$(echo "$DEV" | jqget "['id']")
DEV_CODE=$(echo "$DEV" | jqget "['code']")
DEV_TOKEN=$(echo "$DEV" | jqget "['token']")
[ -n "$DEV_TOKEN" ] && ok "device $DEV_CODE registered"
curl -sf -X POST "$API/device-gateway/telemetry" -H 'Content-Type: application/json' -d "{
  \"device_code\":\"$DEV_CODE\",\"token\":\"$DEV_TOKEN\",
  \"metrics\":{\"temperature\":25.5,\"battery\":88},
  \"position\":{\"lat\":28.18,\"lng\":113.60}}" | grep -q 'accepted' && ok "telemetry accepted"
BAD=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/device-gateway/telemetry" -H 'Content-Type: application/json' \
  -d "{\"device_code\":\"$DEV_CODE\",\"token\":\"wrong\",\"metrics\":{}}")
[ "$BAD" = "401" ] && ok "bad device token rejected"

step "iot: observations auto-registered from telemetry"
curl -sf "$API/observations?device_id=$DEV_ID" -H "$AUTH" | grep -q 'temperature' && ok "observations"

step "mission: safety pipeline (validate -> safety -> approve -> start)"
MISSION=$(curl -sf -X POST "$API/missions" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"device_id\":\"$DEV_ID\",\"name\":\"冒烟喷洒任务\",\"mission_type\":\"spray\",
  \"route\":[{\"lat\":28.18,\"lng\":113.602,\"alt\":40,\"action\":\"takeoff\"},{\"lat\":28.185,\"lng\":113.608,\"alt\":45,\"action\":\"photo\"},{\"lat\":28.188,\"lng\":113.609,\"alt\":40,\"action\":\"land\"}]}")
MISSION_ID=$(echo "$MISSION" | jqget "['id']")
curl -sf -X POST "$API/missions/$MISSION_ID/validate" -H "$AUTH" | grep -q 'safety_check' && ok "validated"
curl -sf -X POST "$API/missions/$MISSION_ID/safety-check" -H "$AUTH" | grep -q 'awaiting_confirmation' && ok "safety passed"
BLOCKED=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/missions/$MISSION_ID/start" -H "$AUTH")
[ "$BLOCKED" = "409" ] && ok "start blocked before approval (state machine)"
curl -sf -X POST "$API/missions/$MISSION_ID/approve" -H "$AUTH" | grep -q '"status":"approved"' && ok "human approval"
curl -sf -X POST "$API/missions/$MISSION_ID/start" -H "$AUTH" | grep -q '"status":"executing"' && ok "mission executing"

step "mission: device-gateway progress -> completion -> trace event"
for P in 33 66 100; do
  curl -sf -X POST "$API/device-gateway/missions/$MISSION_ID/progress" -H 'Content-Type: application/json' -d "{
    \"device_code\":\"$DEV_CODE\",\"token\":\"$DEV_TOKEN\",\"progress\":$P,\"latitude\":28.18,\"longitude\":113.60}" > /dev/null
done
STATUS=$(curl -sf "$API/missions/$MISSION_ID" -H "$AUTH" | jqget "['status']")
[ "$STATUS" = "completed" ] && ok "mission completed"
curl -sf "$API/trace-events?ref_type=mission&ref_id=$MISSION_ID" -H "$AUTH" | grep -q 'uav_mission' && ok "mission trace event"

step "alert rules: threshold triggers on telemetry"
curl -sf -X POST "$API/alert-rules" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"name\":\"冒烟温度规则\",\"metric\":\"temperature\",\"operator\":\">\",\"threshold\":40,\"severity\":\"warning\"}" > /dev/null
curl -sf -X POST "$API/device-gateway/telemetry" -H 'Content-Type: application/json' -d "{
  \"device_code\":\"$DEV_CODE\",\"token\":\"$DEV_TOKEN\",\"metrics\":{\"temperature\":45.2}}" > /dev/null
sleep 1
curl -sf "$API/alerts?status=open" -H "$AUTH" | grep -q 'temperature' && ok "alert created"

step "blockchain proof (mock provider)"
EVID=$(curl -sf -X POST "$API/blockchain-proofs" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"ref_type\":\"package\",\"ref_id\":\"$PKG_ID\"}")
echo "$EVID" | grep -q '"provider":"mock"' && ok "proof anchored"
curl -sf "$API/blockchain-proofs?ref_type=package&ref_id=$PKG_ID" -H "$AUTH" | grep -q 'confirmed' && ok "proof listed"

step "sync: mock platform + sync task -> SYNCED"
PLATFORMS=$(curl -sf "$API/external-platforms" -H "$AUTH")
PLATFORM_ID=$(echo "$PLATFORMS" | jqget "['items'][0]['id']")
curl -sf -X POST "$API/sync-tasks" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"platform_id\":\"$PLATFORM_ID\",\"ref_type\":\"product_batch\",\"ref_id\":\"$PB_ID\",\"action\":\"push\"}" > /dev/null
curl -sf -X POST "$API/sync-tasks/run" -H "$AUTH" > /dev/null
sleep 1
SYNC_STATUS=$(curl -sf "$API/sync-tasks?ref_id=$PB_ID" -H "$AUTH" | jqget "['items'][0]['status']")
[ "$SYNC_STATUS" = "SYNCED" ] && ok "sync task SYNCED (local-first verified)"

step "ai: mock chat returns labeled recommendation"
curl -sf -X POST "$API/ai/chat" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"北坡地块最近该做什么农事？"}]}' | grep -q 'RECOMMENDATION' && ok "ai chat"

step "audit: write operations recorded"
curl -sf "$API/audit-logs?resource_type=farms" -H "$AUTH" | grep -q '"action"' && ok "audit log"

# ---------- SaaS multi-tenant isolation (ADR-006) ----------

step "saas: self-registration provisions tenant B (auto-login)"
REG=$(curl -sf -X POST "$API/auth/register" -H 'Content-Type: application/json' -d "{
  \"tenant_name\":\"冒烟租户B\",\"username\":\"smoke-$TAG\",\"password\":\"pass123456\",\"display_name\":\"B租户管理员\"}")
B_TOKEN=$(echo "$REG" | jqget "['tokens']['access_token']")
B_AUTH="Authorization: Bearer $B_TOKEN"
B_CODE=$(echo "$REG" | jqget "['tenant_code']")
[ -n "$B_TOKEN" ] && ok "tenant B registered (code $B_CODE)"

step "saas: B logs in explicitly with tenant_code"
B_LOGIN=$(curl -sf -X POST "$API/auth/login" -H 'Content-Type: application/json' -d "{
  \"username\":\"smoke-$TAG\",\"password\":\"pass123456\",\"tenant_code\":\"$B_CODE\"}")
B_TOKEN=$(echo "$B_LOGIN" | jqget "['tokens']['access_token']")
[ -n "$B_TOKEN" ] && ok "explicit tenant_code login"

step "saas: cross-tenant READ is denied (B reads demo farm -> 404)"
DEMO_FARM=$(curl -sf "$API/farms?page=1&page_size=1" -H "$AUTH" | jqget "['items'][0]['id']")
XCODE=$(curl -s -o /dev/null -w '%{http_code}' "$API/farms/$DEMO_FARM" -H "$B_AUTH")
[ "$XCODE" = "404" ] && ok "cross-tenant read 404"

step "saas: cross-tenant role WRITE is denied (B patches demo user -> 403)"
DEMO_USER=$(curl -sf "$API/users?page=1&page_size=1" -H "$AUTH" | jqget "['items'][0]['id']")
XCODE=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$API/users/$DEMO_USER" -H "$B_AUTH" \
  -H 'Content-Type: application/json' -d '{"roles":["operator"]}')
[ "$XCODE" = "403" ] && ok "cross-tenant user_roles write 403"

step "saas: cross-tenant trace-code minting is denied (B references demo package -> 400)"
XCODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/trace-codes" -H "$B_AUTH" \
  -H 'Content-Type: application/json' -d "{\"ref_type\":\"package\",\"ref_id\":\"$PKG_ID\",\"code_type\":\"consumer\"}")
[ "$XCODE" = "400" ] && ok "cross-tenant reference injection 400"

step "saas: platform admin login + tenant list"
P_LOGIN=$(curl -sf -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"platform","password":"platform123"}')
P_TOKEN=$(echo "$P_LOGIN" | jqget "['tokens']['access_token']")
[ -n "$P_TOKEN" ] && ok "platform admin login"
P_AUTH="Authorization: Bearer $P_TOKEN"
curl -sf "$API/platform/tenants" -H "$P_AUTH" | grep -q '"code":"demo"' && ok "tenant list contains demo"
curl -sf "$API/platform/tenants" -H "$P_AUTH" | grep -q "$B_CODE" && ok "tenant list contains registered tenant"

step "saas: platform creates a tenant"
P_CREATE=$(curl -sf -X POST "$API/platform/tenants" -H "$P_AUTH" -H 'Content-Type: application/json' -d "{
  \"name\":\"平台创建租户\",\"code\":\"plat-$TAG\",\"admin_username\":\"pa-$TAG\",\"admin_password\":\"pass123456\"}")
PLAT_CODE=$(echo "$P_CREATE" | jqget "['code']")
[ -n "$PLAT_CODE" ] && ok "platform-provisioned tenant $PLAT_CODE"

step "saas: suspend tenant B -> its admin login fails -> reactivate"
B_TENANT_ID=$(curl -sf "$API/platform/tenants?search=$B_CODE" -H "$P_AUTH" | jqget "['items'][0]['id']")
curl -sf -X PATCH "$API/platform/tenants/$B_TENANT_ID" -H "$P_AUTH" -H 'Content-Type: application/json' \
  -d '{"status":"suspended"}' > /dev/null
SUSPENDED=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"smoke-$TAG\",\"password\":\"pass123456\",\"tenant_code\":\"$B_CODE\"}")
[ "$SUSPENDED" = "401" ] && ok "suspended tenant login rejected"
curl -sf -X PATCH "$API/platform/tenants/$B_TENANT_ID" -H "$P_AUTH" -H 'Content-Type: application/json' \
  -d '{"status":"active"}' > /dev/null && ok "tenant re-activated"

step "saas: ambiguous login without tenant_code -> 409 MULTI_TENANT_AMBIGUOUS"
curl -sf -X POST "$API/users" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"username\":\"dup-$TAG\",\"password\":\"duppass123\",\"roles\":[\"operator\"]}" > /dev/null
curl -sf -X POST "$API/users" -H "$B_AUTH" -H 'Content-Type: application/json' \
  -d "{\"username\":\"dup-$TAG\",\"password\":\"duppass456\",\"roles\":[\"operator\"]}" > /dev/null
AMB=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"dup-$TAG\",\"password\":\"duppass123\"}")
[ "$AMB" = "409" ] && ok "ambiguous login returns 409 with tenant options"

step "remote sensing: upload imagery + create processing job"
IMG=$(curl -sf -X POST "$API/files" -H "$AUTH" -F "file=@tests/e2e/fixtures/sample-image.txt;type=image/tiff")
IMG_ID=$(echo "$IMG" | jqget "['id']")
[ -n "$IMG_ID" ] && ok "imagery uploaded ($IMG_ID)"
RS=$(curl -sf -X POST "$API/remote-sensing/jobs" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"job_type\":\"orthophoto\",\"file_ids\":[\"$IMG_ID\"],\"processor\":\"mock\"}")
RS_ID=$(echo "$RS" | jqget "['id']")
[ -n "$RS_ID" ] && ok "job queued ($RS_ID)"

step "remote sensing: worker processes job -> succeeded"
deadline=$(($(date +%s) + 15))
RS_STATUS="queued"
while [ "$(date +%s)" -lt "$deadline" ]; do
  RS_STATUS=$(curl -sf "$API/remote-sensing/jobs/$RS_ID" -H "$AUTH" | jqget "['status']")
  [ "$RS_STATUS" = "succeeded" ] && break
  sleep 1
done
[ "$RS_STATUS" = "succeeded" ] && ok "job succeeded via mock processor"

step "remote sensing: STAC catalog exposes the orthophoto asset"
curl -sf "$API/stac/collections" -H "$AUTH" | grep -q 'orthophoto' && ok "collection listed"
curl -sf "$API/stac/collections/orthophoto/items" -H "$AUTH" | grep -q "$RS_ID" && ok "stac item published"

printf '\n\033[32m===== SMOKE RESULT: %d steps passed =====\033[0m\n' "$PASS"
