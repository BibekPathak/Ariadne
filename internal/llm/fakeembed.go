package llm

import (
	"hash/fnv"
	"math"
	"strings"
)

// embeddingDim is the fixed dimension of deterministic fake embeddings.
const embeddingDim = 128

// DeterministicEmbed produces bag-of-words embeddings: each token hashes into
// a fixed-dimension bucket, then the vector is L2-normalised. Vectors for
// texts sharing tokens end up similar, which lets the offline demo exercise
// semantic retrieval meaningfully without a real embedding model.
func DeterministicEmbed(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, embeddingDim)
		for _, token := range tokenize(text) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(token))
			idx := int(h.Sum32()) % embeddingDim
			v[idx]++
		}
		norm := 0.0
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if norm > 0 {
			inv := float32(1.0 / math.Sqrt(norm))
			for j := range v {
				v[j] *= inv
			}
		}
		out[i] = v
	}
	return out
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}
