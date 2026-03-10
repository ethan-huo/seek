package search

import (
	"github.com/anthropics/seek/internal/embed"
	"github.com/anthropics/seek/internal/store"
)

const (
	DefaultLimit = 20
	RRFk         = 60
)

type Engine struct {
	store       *store.Store
	embedClient *embed.Client
	vlClient    *embed.VLClient
}

func NewEngine(s *store.Store, ec *embed.Client) *Engine {
	return &Engine{store: s, embedClient: ec}
}

// NewEngineWithVL creates a search engine with a VL client for multimodal query embedding.
func NewEngineWithVL(s *store.Store, ec *embed.Client, vlc *embed.VLClient) *Engine {
	return &Engine{store: s, embedClient: ec, vlClient: vlc}
}

// SearchBM25 performs BM25 full-text search.
func (e *Engine) SearchBM25(query string, limit int) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return e.store.SearchFTS(query, limit)
}

// SearchVector performs vector semantic search.
func (e *Engine) SearchVector(query string, limit int) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	// Prefer VL client if available (unified vector space for multimodal)
	if e.vlClient != nil {
		qEmb, err := e.vlClient.EmbedText(query)
		if err != nil {
			return nil, err
		}
		return e.store.SearchVector(qEmb, limit)
	}

	if e.embedClient == nil {
		return nil, nil
	}

	qEmb, err := e.embedClient.EmbedSingle(query)
	if err != nil {
		return nil, err
	}

	return e.store.SearchVector(qEmb, limit)
}

// SearchHybrid performs hybrid search using RRF fusion.
func (e *Engine) SearchHybrid(query string, limit int) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	bm25Results, err := e.SearchBM25(query, limit*2)
	if err != nil {
		return nil, err
	}

	vecResults, err := e.SearchVector(query, limit*2)
	if err != nil {
		// Fall back to BM25 only if vector search fails
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}

	return rrfFusion(bm25Results, vecResults, limit), nil
}

func rrfFusion(bm25, vec []store.SearchResult, limit int) []store.SearchResult {
	// Key by DocumentID for document-level fusion.
	// BM25 returns ChunkID==0 (document-level), vector returns real chunk IDs.
	// Using docID ensures both branches can merge for the same document.
	scores := make(map[int64]float64)
	resultMap := make(map[int64]store.SearchResult)

	for rank, r := range bm25 {
		scores[r.DocumentID] += 1.0 / float64(RRFk+rank+1)
		resultMap[r.DocumentID] = r
	}

	for rank, r := range vec {
		scores[r.DocumentID] += 1.0 / float64(RRFk+rank+1)
		if _, exists := resultMap[r.DocumentID]; !exists {
			resultMap[r.DocumentID] = r
		}
	}

	// Sort by RRF score
	type scored struct {
		docID int64
		score float64
	}
	var sorted []scored
	for id, s := range scores {
		sorted = append(sorted, scored{id, s})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	results := make([]store.SearchResult, len(sorted))
	for i, s := range sorted {
		r := resultMap[s.docID]
		r.Score = s.score
		results[i] = r
	}

	return results
}
