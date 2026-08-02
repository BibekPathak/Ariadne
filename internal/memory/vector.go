package memory

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// VectorStore is the Go-native semantic memory backend. It is a flat index
// with cosine similarity, persisted as JSON. It exists to keep the platform
// runnable without extra infrastructure; the SemanticStore interface is the
// seam a production backend (Qdrant) implements later.
type VectorStore struct {
	mu     sync.Mutex
	dir    string
	points []vectorPoint
}

type vectorPoint struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Kind      string    `json:"kind"`
	Topic     string    `json:"topic"`
	Content   string    `json:"content"`
	Vector    []float32 `json:"vector"`
	CreatedAt time.Time `json:"created_at"`
}

func NewVectorStore(dir string) (*VectorStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	v := &VectorStore{dir: dir}
	if err := v.load(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *VectorStore) path() string { return filepath.Join(v.dir, "vectors.json") }

func (v *VectorStore) load() error {
	raw, err := os.ReadFile(v.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(raw, &v.points)
}

func (v *VectorStore) persist() error {
	raw, err := json.Marshal(v.points)
	if err != nil {
		return err
	}
	return os.WriteFile(v.path(), raw, 0o644)
}

func (v *VectorStore) Index(ctx context.Context, items []IndexItem) error {
	if len(items) == 0 {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, it := range items {
		if it.ID == "" {
			it.ID = NewID()
		}
		v.points = append(v.points, vectorPoint{
			ID: it.ID, AgentID: it.AgentID, Kind: it.Kind, Topic: it.Topic,
			Content: it.Content, Vector: it.Vector, CreatedAt: time.Now().UTC(),
		})
	}
	return v.persist()
}

func (v *VectorStore) Search(ctx context.Context, agentID string, query []float32, limit int) ([]Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	type hit struct {
		score float32
		point vectorPoint
	}
	var hits []hit
	for _, p := range v.points {
		if p.AgentID != "" && p.AgentID != agentID {
			continue
		}
		s := cosine(p.Vector, query)
		if s > 0.01 {
			hits = append(hits, hit{score: s, point: p})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, 0, len(hits))
	for _, h := range hits {
		out = append(out, Entry{
			ID: h.point.ID, AgentID: h.point.AgentID, Kind: h.point.Kind,
			Topic: h.point.Topic, Content: h.point.Content, Score: h.score,
			CreatedAt: h.point.CreatedAt,
		})
	}
	return out, nil
}

func (v *VectorStore) Close() error { return nil }

func cosine(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
