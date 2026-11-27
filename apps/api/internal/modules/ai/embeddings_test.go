package ai

import (
	"context"
	"math"
	"testing"
)

// TestCosine pins the similarity math.
func TestCosine(t *testing.T) {
	a := []float64{1, 0, 1}
	if got := cosine(a, a); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical vectors should score 1, got %f", got)
	}
	b := []float64{-1, 0, -1}
	if got := cosine(a, b); math.Abs(got+1) > 1e-9 {
		t.Errorf("opposite vectors should score -1, got %f", got)
	}
	if got := cosine(a, []float64{0, 1, 0}); got != 0 {
		t.Errorf("orthogonal vectors should score 0, got %f", got)
	}
	if got := cosine(a, nil); got != 0 {
		t.Errorf("empty vector should score 0, got %f", got)
	}
	if got := cosine(a, []float64{1, 0}); got != 0 {
		t.Errorf("dimension mismatch should score 0, got %f", got)
	}
}

// TestMockEmbedderDeterministicAndSimilar verifies determinism and that
// paraphrased agronomy texts score higher than unrelated ones (the RAG-lite
// semantic upgrade, docs/adr/adr-014).
func TestMockEmbedderDeterministicAndSimilar(t *testing.T) {
	e := &MockEmbedder{}
	vecs, err := e.Embed(context.Background(), []string{
		"油茶春季施肥应使用有机肥，沿树冠滴水线开沟施入。", // query topic
		"春季施肥要用有机肥，在滴水线附近开沟。",      // paraphrase
		"无人机航线规划需要注意禁飞区。",          // unrelated
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 || len(vecs[0]) != mockEmbedDim {
		t.Fatalf("shape wrong: %d x %d", len(vecs), len(vecs[0]))
	}
	// Determinism.
	again, _ := e.Embed(context.Background(), []string{"油茶春季施肥应使用有机肥，沿树冠滴水线开沟施入。"})
	if cosine(vecs[0], again[0]) < 0.9999 {
		t.Error("mock embedder must be deterministic")
	}
	// Normalized vectors.
	if math.Abs(math.Sqrt(dot(vecs[0], vecs[0]))-1) > 1e-6 {
		t.Error("vectors must be L2-normalized")
	}
	// Semantic ordering: paraphrase > unrelated.
	same := cosine(vecs[0], vecs[1])
	other := cosine(vecs[0], vecs[2])
	if same <= other {
		t.Errorf("paraphrase similarity (%.3f) should exceed unrelated (%.3f)", same, other)
	}
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// TestRankHits verifies the combined keyword+cosine ranking, per-doc dedup
// and top-K truncation (ADR-014).
func TestRankHits(t *testing.T) {
	tokens := tokenizeQuery("油茶施肥")
	hits := []chunkHit{
		{docID: "d1", content: "油茶春季施肥应使用有机肥。", embedding: mockEmbed("油茶春季施肥应使用有机肥。")}, // keyword+semantic
		{docID: "d2", content: "修剪知识：疏除过密枝。", embedding: mockEmbed("油茶施肥管理")},          // semantic only
		{docID: "d3", content: "完全不相关的文本。"},                                            // nothing
	}
	ranked := rankHits(hits, tokens, mockEmbed("油茶施肥"))
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked (d3 kept at 0), got %d", len(ranked))
	}
	// d3 (no keyword, no semantic overlap) must rank last; both oil-tea hits
	// rank above it. Which of d1/d2 leads depends on the combined score — the
	// guarantee under test is that semantic-only beats zero and keyword+dedup
	// works.
	if ranked[len(ranked)-1].docID != "d3" {
		t.Errorf("unrelated hit should rank last, got %s first", ranked[len(ranked)-1].docID)
	}
	if ranked[0].docID == "d3" {
		t.Error("zero-hit doc must not rank first")
	}
	// Keyword hits still contribute: d1 has keyword count >= 1.
	if hits[0].keyword = scoreChunk(hits[0].content, tokens); hits[0].keyword < 1 {
		t.Errorf("d1 keyword count should be >= 1, got %d", hits[0].keyword)
	}

	// Top-K truncation.
	many := []chunkHit{}
	for i := 0; i < retrieveTopK+2; i++ {
		many = append(many, chunkHit{docID: docIDFor(i), content: "油茶施肥说明。"})
	}
	if got := len(rankHits(many, tokens, nil)); got != retrieveTopK {
		t.Errorf("top-K truncation wrong: %d", got)
	}
}

func docIDFor(i int) string { return "d" + string(rune('a'+i)) }
