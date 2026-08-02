package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}
		var req embeddingsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "text-embedding-3-small" {
			t.Fatalf("unexpected model %q", req.Model)
		}
		vecs := DeterministicEmbed(req.Input)
		data := make([]map[string]any, len(vecs))
		for i, v := range vecs {
			data[i] = map[string]any{"index": i, "embedding": v}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	p := NewOpenAICompatible(Config{
		Name: "requesty", BaseURL: srv.URL, APIKey: "test-key",
		EmbeddingModel: "text-embedding-3-small",
	})
	out, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || len(out[0]) != embeddingDim {
		t.Fatalf("expected 2 vectors of dim %d, got %d x %d", embeddingDim, len(out), len(out[0]))
	}
}

func TestEmbedRejectsPartialResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": make([]float32, 8)}}})
	}))
	defer srv.Close()
	p := NewOpenAICompatible(Config{BaseURL: srv.URL})
	if _, err := p.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("expected error when a vector is missing")
	}
}

func TestDeterministicEmbedSimilarity(t *testing.T) {
	a := DeterministicEmbed([]string{"table driven tests"})[0]
	b := DeterministicEmbed([]string{"use table driven tests in go"})[0]
	c := DeterministicEmbed([]string{"deploy kubernetes clusters"})[0]
	if cosineSim(a, b) <= cosineSim(a, c) {
		t.Fatal("similar texts should be more similar than unrelated texts")
	}
}

func cosineSim(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
