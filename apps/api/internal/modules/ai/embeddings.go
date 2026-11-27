// Embeddings for semantic retrieval (docs/adr/adr-014): deterministic mock
// embedder (hashed character shingles, 64 dims — free/offline/deterministic
// for CI) and an OpenAI-compatible embeddings client (POST {base}/v1/embeddings)
// for real deployments. Vectors are stored as jsonb on knowledge_chunks and
// ranked with in-Go cosine similarity; pgvector + HNSW is the documented
// scale upgrade once the deployment image ships the extension.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder produces embedding vectors for a batch of texts.
type Embedder interface {
	Name() string
	Dim() int
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

const mockEmbedDim = 64

// MockEmbedder is deterministic: lowercase character shingles (size 3) hashed
// into fixed-dim buckets, L2-normalized. Similar texts land near each other
// without any external service.
type MockEmbedder struct{}

func (p *MockEmbedder) Name() string { return "mock" }
func (p *MockEmbedder) Dim() int     { return mockEmbedDim }

func (p *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		out = append(out, mockEmbed(text))
	}
	return out, nil
}

func mockEmbed(text string) []float64 {
	vec := make([]float64, mockEmbedDim)
	lower := strings.ToLower(strings.TrimSpace(text))
	runes := []rune(lower)
	// character shingles of size 3 (short texts degrade to size 1/2)
	shingle := 3
	if len(runes) < shingle {
		shingle = len(runes)
	}
	if shingle == 0 {
		return vec
	}
	for i := 0; i+shingle <= len(runes); i++ {
		h := fnv.New32a()
		_, _ = h.Write([]byte(string(runes[i : i+shingle])))
		vec[h.Sum32()%mockEmbedDim] += 1
	}
	// L2 normalize
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// OpenAIEmbedder calls any OpenAI-compatible /v1/embeddings endpoint
// (OpenAI, vLLM, Ollama, ...). Model comes from configuration.
type OpenAIEmbedder struct {
	BaseURL     string
	APIKey      string
	Model       string
	DimOverride int // 0 = provider default (text-embedding-3-small: 1536)
}

func (p *OpenAIEmbedder) Name() string { return "openai-embeddings" }

func (p *OpenAIEmbedder) Dim() int {
	if p.DimOverride > 0 {
		return p.DimOverride
	}
	return 1536
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (p *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if p.BaseURL == "" || p.APIKey == "" {
		return nil, fmt.Errorf("embeddings base url / api key not configured")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	body, _ := json.Marshal(embeddingsRequest{Model: p.Model, Input: texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings status %d", resp.StatusCode)
	}
	var parsed embeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings count mismatch: %d != %d", len(parsed.Data), len(texts))
	}
	out := make([][]float64, len(texts))
	for _, d := range parsed.Data {
		out[d.Index] = d.Embedding
	}
	return out, nil
}

// cosine returns the cosine similarity of two vectors (0 when either is empty).
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	dot, na, nb := 0.0, 0.0, 0.0
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
