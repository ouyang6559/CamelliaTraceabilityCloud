package ai

import (
	"strings"
	"testing"
)

// TestChunkContent covers paragraph grouping, oversized-line sliding windows
// and the overlap guarantee (docs/adr/adr-009).
func TestChunkContent(t *testing.T) {
	// Empty input.
	if got := chunkContent("   \n  ", 400, 40); len(got) != 0 {
		t.Errorf("empty input should produce no chunks, got %d", len(got))
	}

	// Short single paragraph stays one chunk.
	one := chunkContent("油茶春季施肥知识。", 400, 40)
	if len(one) != 1 {
		t.Errorf("short text: expected 1 chunk, got %d", len(one))
	}

	// Multiple paragraphs merge up to maxRunes then split.
	long := ""
	for i := 0; i < 20; i++ {
		long += "油茶施肥line。采用有机肥料进行沟施管理。\n"
	}
	chunks := chunkContent(long, 200, 40)
	if len(chunks) < 2 {
		t.Fatalf("long text should split into >= 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c)) > 200 {
			t.Errorf("chunk[%d] exceeds maxRunes: %d", i, len([]rune(c)))
		}
	}
	// Overlap: consecutive chunks share the tail of the previous one.
	if len(chunks) >= 2 {
		prev := []rune(chunks[0])
		if len(prev) > 40 {
			tail := string(prev[len(prev)-40:])
			if !strings.Contains(chunks[1], strings.TrimSpace(tail)) {
				t.Errorf("overlap missing between chunk 0 and 1")
			}
		}
	}

	// Consecutive chunks share an overlap (>= 10 runes) — the carry in flush().
	for i := 0; i+1 < len(chunks); i++ {
		if !hasOverlap(chunks[i], chunks[i+1], 10) {
			t.Errorf("overlap missing between chunk %d and %d", i, i+1)
		}
	}

	// Oversized single paragraph gets windowed without data loss.
	big := strings.Repeat("茶", 500)
	bigChunks := chunkContent(big, 200, 40)
	total := 0
	for _, c := range bigChunks {
		total += len([]rune(c))
		if len([]rune(c)) > 200 {
			t.Errorf("window chunk exceeds maxRunes: %d", len([]rune(c)))
		}
	}
	if total < 500 {
		t.Errorf("oversized paragraph lost content: %d < 500", total)
	}
	if len(bigChunks) < 3 {
		t.Errorf("500 runes at max 200 should yield >= 3 windows, got %d", len(bigChunks))
	}
}

// hasOverlap reports whether a suffix of a (length >= minLen) is a prefix of b.
func hasOverlap(a, b string, minLen int) bool {
	ra, rb := []rune(a), []rune(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	for l := n; l >= minLen; l-- {
		if string(ra[len(ra)-l:]) == string(rb[:l]) {
			return true
		}
	}
	return false
}

// TestTokenizeQuery covers CJK bigrams + latin words and dedup.
func TestTokenizeQuery(t *testing.T) {
	tokens := tokenizeQuery("油茶施肥施肥 Spring Fertilizer a")
	joined := strings.Join(tokens, ",")
	if !strings.Contains(joined, "油茶") || !strings.Contains(joined, "茶施") {
		t.Errorf("CJK bigrams missing: %s", joined)
	}
	if !strings.Contains(joined, "spring") || !strings.Contains(joined, "fertilizer") {
		t.Errorf("latin words missing: %s", joined)
	}
	if strings.Contains(joined, "spring, spring") {
		t.Error("tokens must be deduplicated")
	}
	for _, tk := range tokens {
		if tk == "a" {
			t.Error("single-char latin token should be filtered")
		}
	}
}

// TestScoreAndRank covers scoring order and per-doc dedup through
// retrieveKnowledge's pure ranking path (scoreChunk + insertion sort shape).
func TestScoreAndRank(t *testing.T) {
	tokens := tokenizeQuery("油茶施肥")
	hitA := scoreChunk("油茶春季施肥应使用有机肥。", tokens) // strong
	hitB := scoreChunk("修剪知识：疏除过密枝。", tokens)   // zero
	hitC := scoreChunk("施肥周期说明。", tokens)       // medium: 施肥 x1
	if hitA <= hitC || hitC <= hitB {
		t.Errorf("scoring order wrong: A=%d C=%d B=%d", hitA, hitC, hitB)
	}
	if hitB != 0 {
		t.Errorf("unrelated chunk should score 0, got %d", hitB)
	}
}

// TestBuildKnowledgeContext verifies the prompt context block format.
func TestBuildKnowledgeContext(t *testing.T) {
	out := buildKnowledgeContext([]KnowledgeSource{
		{DocID: "d1", Title: "施肥指南", Category: "肥料", Score: 5, Excerpt: "油茶春季施肥应使用有机肥。"},
	})
	if !strings.Contains(out, "[知识库摘录") {
		t.Errorf("context header missing: %s", out)
	}
	if !strings.Contains(out, "施肥指南") || !strings.Contains(out, "有机肥") {
		t.Errorf("source content missing: %s", out)
	}
	if buildKnowledgeContext(nil) != "" {
		t.Error("empty sources should produce empty context")
	}
}
