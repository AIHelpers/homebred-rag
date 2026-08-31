package usecase

import "sort"

// RRFConstant is the standard damping constant used in reciprocal rank fusion.
const RRFConstant = 60.0

// HybridSearch runs vector similarity search and keyword (BM25-ish) search
// independently, then fuses the two rankings with Reciprocal Rank Fusion.
// RRF is used (rather than blending raw scores) because vector cosine scores
// and BM25 scores live on incomparable scales; fusing by rank position is
// scale-free and still meaningfully boosts chunks both methods agree on.
func HybridSearch(vector VectorIndex, keyword KeywordIndex, embedder Embedder, queryText string, topK int) []ScoredChunk {
	fetchK := topK * 4
	if fetchK < 20 {
		fetchK = 20
	}

	vecResults := vector.Search(embedder.Embed(queryText), fetchK)
	kwResults := keyword.Search(queryText, fetchK)

	fused := map[string]float64{}
	chunkByID := map[string]ScoredChunk{}

	for rank, r := range vecResults {
		fused[r.Chunk.ID] += 1.0 / (RRFConstant + float64(rank+1))
		chunkByID[r.Chunk.ID] = r
	}
	for rank, r := range kwResults {
		fused[r.Chunk.ID] += 1.0 / (RRFConstant + float64(rank+1))
		if _, ok := chunkByID[r.Chunk.ID]; !ok {
			chunkByID[r.Chunk.ID] = r
		}
	}

	out := make([]ScoredChunk, 0, len(fused))
	for id, score := range fused {
		sc := chunkByID[id]
		sc.Score = score
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}
