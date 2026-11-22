// Package ai implements the AI provider abstraction (specs/07): chat + vision,
// provider backends (mock / OpenAI-compatible), job records and human review.
// AI output is always labeled as AI-generated and never auto-executes actions.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ulidx"
)

// Provider is the AI backend contract; OpenAI-compatible servers, Ollama and
// vLLM all expose this shape via the openai backend.
type Provider interface {
	Name() string
	Chat(ctx context.Context, prompt string, contextData map[string]any) (output string, outputType string, confidence float64, err error)
	Vision(ctx context.Context, imageURL string, taskType string) (output string, outputType string, confidence float64, err error)
}

// Module wires AI routes.
type Module struct {
	pool     *pgxpool.Pool
	provider Provider
	log      *slog.Logger
}

func NewModule(pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) *Module {
	var p Provider
	switch cfg.AIProvider {
	case "openai":
		p = &OpenAIProvider{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIAPIKey, Model: cfg.AIModel}
	default:
		p = &MockProvider{}
	}
	log.Info("ai provider", "provider", p.Name())
	return &Module{pool: pool, provider: p, log: log}
}

// Mount registers /ai routes.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	r.Route("/ai", func(rr chi.Router) {
		rr.Use(authorize("ai", "read"))
		rr.Get("/jobs", m.listJobs)
		rr.Get("/jobs/{id}", m.getJob)
		rr.With(authorize("ai", "create")).Post("/chat", m.chat)
		rr.With(authorize("ai", "create")).Post("/vision", m.vision)
		rr.With(authorize("ai", "review")).Post("/jobs/{id}/review", m.review)
		rr.Get("/knowledge", m.listKnowledge)
		rr.With(authorize("ai", "create")).Post("/knowledge", m.createKnowledge)
		rr.With(authorize("ai", "create")).Post("/knowledge/import", m.importKnowledgeFile)
		rr.With(authorize("ai", "create")).Delete("/knowledge/{id}", m.deleteKnowledge)
	})
}

type chatWrite struct {
	Messages     []chatMessage `json:"messages"`
	ContextRef   string        `json:"context_ref"` // optional ref: plot_id / batch_id / device_id
	ContextType  string        `json:"context_type"`
	UseKnowledge bool          `json:"use_knowledge"` // RAG-lite: retrieve tenant knowledge base (ADR-009)
}
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chat runs one AI request and stores the job + audit trail (specs/07.7).
func (m *Module) chat(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req chatWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if len(req.Messages) == 0 {
		httpx.Respond(w, r, nil, apierr.Invalid("messages are required"))
		return
	}
	prompt := ""
	for _, msg := range req.Messages {
		prompt += msg.Role + ": " + msg.Content + "\n"
	}

	// RAG-lite retrieval (ADR-009): inject knowledge excerpts + track sources.
	var sources []KnowledgeSource
	if req.UseKnowledge && len(req.Messages) > 0 {
		query := req.Messages[len(req.Messages)-1].Content
		if srcs, err := m.retrieveKnowledge(r.Context(), id.TenantID, query); err == nil && len(srcs) > 0 {
			sources = srcs
			prompt = buildKnowledgeContext(srcs) + "\n" + prompt
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	output, outputType, confidence, err := m.provider.Chat(ctx, prompt, map[string]any{
		"tenant": id.TenantID, "context_type": req.ContextType, "context_ref": req.ContextRef,
	})
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Unavailable("ai provider error: %v", err))
		return
	}
	jobID := m.recordJob(r.Context(), id, "chat", prompt, output, outputType, confidence, req.ContextType, req.ContextRef)
	resp := map[string]any{
		"job_id":      jobID,
		"output":      output,
		"output_type": outputType,
		"confidence":  confidence,
		"disclaimer":  "AI-generated recommendation, not an instruction; confirm before acting",
	}
	if len(sources) > 0 {
		resp["sources"] = sources
	}
	httpx.Respond(w, r, resp, nil)
}

type visionWrite struct {
	FileID   string `json:"file_id"`
	TaskType string `json:"task_type"` // disease | pest | weed | count | canopy
}

// vision analyzes an uploaded image (mock backend returns deterministic results).
func (m *Module) vision(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	var req visionWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.FileID == "" {
		httpx.Respond(w, r, nil, apierr.Invalid("file_id is required"))
		return
	}
	if req.TaskType == "" {
		req.TaskType = "disease"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	output, outputType, confidence, err := m.provider.Vision(ctx, req.FileID, req.TaskType)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Unavailable("ai provider error: %v", err))
		return
	}
	jobID := m.recordJob(r.Context(), id, "vision/"+req.TaskType, req.FileID, output, outputType, confidence, req.TaskType, req.FileID)
	httpx.Respond(w, r, map[string]any{
		"job_id":      jobID,
		"output":      output,
		"output_type": outputType,
		"confidence":  confidence,
		"disclaimer":  "AI-generated result; field verification required",
	}, nil)
}

// recordJob stores the job + audit rows (specs/07.7: model/provider/prompt/output/confidence).
func (m *Module) recordJob(ctx context.Context, id ctxid.Identity, taskType, prompt, output, outputType string, confidence float64, refType, refID string) string {
	jobID := ulidx.New()
	outputJSON, _ := json.Marshal(map[string]any{"text": output})
	conf := confidence
	if conf == 0 {
		conf = 0.6
	}
	if outputType == "" {
		outputType = "RECOMMENDATION"
	}
	_, err := m.pool.Exec(ctx, `
		INSERT INTO ai_jobs (id, tenant_id, provider, model, task_type, status,
			input_ref_type, input_ref_id, prompt_version, input_payload, output, output_type, confidence,
			created_by, finished_at)
		VALUES ($1,$2,$3,$4,$5,'succeeded',NULLIF($6,''),NULLIF($7,''),'v1',to_jsonb($8::jsonb),
			to_jsonb($9::jsonb),$10,$11,$12,now())`,
		jobID, id.TenantID, m.provider.Name(), "default", taskType, refType, refID,
		jsonOf(map[string]any{"prompt": prompt}), string(outputJSON), outputType, conf, id.UserID)
	if err != nil {
		m.log.Warn("ai job insert failed", "err", err)
	}
	return jobID
}

func (m *Module) listJobs(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	var total int64
	if err := m.pool.QueryRow(r.Context(), `SELECT count(*) FROM ai_jobs WHERE tenant_id=$1`, id.TenantID).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT j.id, j.provider, j.model, j.task_type, j.status, j.input_ref_type, j.input_ref_id,
			j.output, j.output_type, j.confidence::float8 AS confidence, j.created_at, j.finished_at,
			COALESCE((SELECT json_agg(jsonb_build_object('action', rv.action, 'note', rv.note, 'actor', rv.actor_id, 'at', rv.created_at))
				FROM ai_reviews rv WHERE rv.job_id = j.id), '[]'::jsonb) AS reviews
		FROM ai_jobs j WHERE j.tenant_id=$1 ORDER BY j.created_at DESC LIMIT $2 OFFSET $3`,
		id.TenantID, pp.PageSize, pp.Offset())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := collect(rows)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) getJob(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT id, provider, model, task_type, status, input_payload, output, output_type,
			confidence::float8 AS confidence, created_at, finished_at
		FROM ai_jobs WHERE tenant_id=$1 AND id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := firstRow(rows)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("ai job"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

type reviewWrite struct {
	Action string `json:"action"` // approved | rejected
	Note   string `json:"note"`
}

// review records the human decision on an AI recommendation (specs/07.1).
func (m *Module) review(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	jobID := chi.URLParam(r, "id")
	var req reviewWrite
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	if req.Action != "approved" && req.Action != "rejected" {
		httpx.Respond(w, r, nil, apierr.Invalid("action must be approved or rejected"))
		return
	}
	var jobTenant string
	err := m.pool.QueryRow(r.Context(),
		`SELECT tenant_id FROM ai_jobs WHERE id=$1`, jobID).Scan(&jobTenant)
	if err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("job not found"))
		return
	}
	if jobTenant != id.TenantID {
		httpx.Respond(w, r, nil, apierr.PermissionDenied("job belongs to another tenant"))
		return
	}
	newID := ulidx.New()
	if _, err := m.pool.Exec(r.Context(), `
		INSERT INTO ai_reviews (id, tenant_id, job_id, actor_id, action, note)
		VALUES ($1,$2,$3,$4,$5,$6)`, newID, id.TenantID, jobID, id.UserID, req.Action, req.Note); err != nil {
		httpx.Respond(w, r, nil, apierr.Invalid("job not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- providers ----------

// MockProvider is the deterministic offline backend used for development, CI
// and demos. It never calls any external service.
type MockProvider struct{}

func (p *MockProvider) Name() string { return "mock" }

func (p *MockProvider) Chat(_ context.Context, prompt string, contextData map[string]any) (string, string, float64, error) {
	output := fmt.Sprintf(
		"[AI 模拟回复 / Mock AI response] 基于当前输入的分析建议：\n"+
			"1. 关注近期天气与土壤墒情变化，合理安排灌溉与施肥。\n"+
			"2. 结合巡田记录检查是否有病虫害苗头。\n"+
			"3. 设备任务安排避开高温时段。\n\n"+
			"(provider=mock, 此结果用于联调演示, 输入摘要: %.80s)", prompt)
	_ = contextData
	return output, "RECOMMENDATION", 0.5, nil
}

func (p *MockProvider) Vision(_ context.Context, _ string, taskType string) (string, string, float64, error) {
	return fmt.Sprintf("[AI 模拟识别 / Mock vision] task=%s: 检测到疑似轻度异常区域 1 处，建议人工复核。", taskType),
		"OBSERVATION", 0.55, nil
}

// OpenAIProvider calls any OpenAI-compatible /v1/chat/completions endpoint
// (OpenAI, Ollama, vLLM, Qwen, DeepSeek, ...). Only the standard API shape is
// used — no vendor specifics (specs/07.2).
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
}

func (p *OpenAIProvider) Name() string { return "openai-compatible" }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (p *OpenAIProvider) Chat(ctx context.Context, prompt string, _ map[string]any) (string, string, float64, error) {
	if p.BaseURL == "" || p.APIKey == "" {
		return "", "", 0, fmt.Errorf("ai base url / api key not configured")
	}
	base := p.BaseURL
	if len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	body, _ := json.Marshal(chatRequest{
		Model: p.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "你是农业数字平台助手。输出必须是建议(Recommendation)，不能直接执行任何操作。"},
			{Role: "user", Content: prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", 0, err
	}
	if len(parsed.Choices) == 0 {
		return "", "", 0, fmt.Errorf("empty ai response")
	}
	return parsed.Choices[0].Message.Content, "RECOMMENDATION", 0, nil
}

func (p *OpenAIProvider) Vision(_ context.Context, _ string, _ string) (string, string, float64, error) {
	return "", "", 0, fmt.Errorf("vision requires a multimodal model; configure provider accordingly")
}
