package runbook

import (
	"context"
	"fmt"
	"math"
	"sort"
)

type VectorSearchResult struct {
	ChunkID    string
	DocFile    string
	DocTitle   string
	Heading    string
	Text       string
	Similarity float64
}

type MemoryVectorStore struct {
	chunks  []Chunk
	vectors [][]float32
	dims    int
}

func NewMemoryVectorStore(dims int) *MemoryVectorStore {
	return &MemoryVectorStore{dims: dims}
}

func BuildMemoryIndex(ctx context.Context, embedder EmbeddingClient, chunks []Chunk) (*MemoryVectorStore, error) {
	if embedder == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	if len(chunks) == 0 {
		return NewMemoryVectorStore(0), nil
	}

	const batchSize = 20
	var validChunks []Chunk
	var validVectors [][]float32

	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Text
		}

		vecs, err := embedder.EmbedBatch(ctx, texts)
		if err != nil {
			fmt.Printf("runbook: embedding batch %d-%d failed: %v\n", i, end-1, err)
			continue
		}

		for j, vec := range vecs {
			if vec == nil {
				continue
			}
			validChunks = append(validChunks, batch[j])
			validVectors = append(validVectors, vec)
		}
	}

	if len(validChunks) == 0 {
		return nil, fmt.Errorf("no chunks successfully embedded")
	}

	dims := 0
	if len(validVectors) > 0 && validVectors[0] != nil {
		dims = len(validVectors[0])
	}

	return &MemoryVectorStore{
		chunks:  validChunks,
		vectors: validVectors,
		dims:    dims,
	}, nil
}

func (s *MemoryVectorStore) Search(queryVec []float32, k int) []VectorSearchResult {
	if s == nil || len(s.vectors) == 0 {
		return nil
	}
	if k <= 0 {
		return nil
	}

	type scored struct {
		idx        int
		similarity float64
	}

	scores := make([]scored, len(s.vectors))
	for i, vec := range s.vectors {
		scores[i] = scored{idx: i, similarity: cosineSimilarity(queryVec, vec)}
	}

	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].similarity > scores[j].similarity
	})

	if k > len(scores) {
		k = len(scores)
	}

	results := make([]VectorSearchResult, k)
	for i := 0; i < k; i++ {
		c := s.chunks[scores[i].idx]
		results[i] = VectorSearchResult{
			ChunkID:    c.ID,
			DocFile:    c.DocFile,
			DocTitle:   c.DocTitle,
			Heading:    c.Heading,
			Text:       c.Text,
			Similarity: scores[i].similarity,
		}
	}
	return results
}

func (s *MemoryVectorStore) Len() int {
	if s == nil {
		return 0
	}
	return len(s.chunks)
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct float64
	var normA float64
	var normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
