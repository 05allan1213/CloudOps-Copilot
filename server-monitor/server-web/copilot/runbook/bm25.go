package runbook

import "math"

type BM25Engine struct {
	docTokens [][]string
	df        map[string]int
	idf       map[string]float64
	avgdl     float64
	k1        float64
	b         float64
	docCount  int
}

func NewBM25Engine(docs []Document, k1, b float64) *BM25Engine {
	if k1 <= 0 {
		k1 = 1.2
	}
	if b < 0 || b > 1 {
		b = 0.75
	}

	docCount := len(docs)
	docTokens := make([][]string, docCount)
	df := make(map[string]int)

	for i, doc := range docs {
		tokens := Tokenize(doc.Body)
		docTokens[i] = tokens
		seen := make(map[string]struct{})
		for _, t := range tokens {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				df[t]++
			}
		}
	}

	idf := make(map[string]float64, len(df))
	for term, n := range df {
		idf[term] = math.Log(1 + (float64(docCount)-float64(n)+0.5)/(float64(n)+0.5))
	}

	var totalLen int
	for _, tokens := range docTokens {
		totalLen += len(tokens)
	}
	var avgdl float64
	if docCount > 0 {
		avgdl = float64(totalLen) / float64(docCount)
	}

	return &BM25Engine{
		docTokens: docTokens,
		df:        df,
		idf:       idf,
		avgdl:     avgdl,
		k1:        k1,
		b:         b,
		docCount:  docCount,
	}
}

func (e *BM25Engine) Score(queryTokens []string, docIdx int) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	if docIdx < 0 || docIdx >= e.docCount {
		return 0
	}
	if e.avgdl == 0 {
		return 0
	}

	tokens := e.docTokens[docIdx]
	dl := float64(len(tokens))

	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}

	var score float64
	for _, qt := range queryTokens {
		idfVal, ok := e.idf[qt]
		if !ok {
			continue
		}
		f := float64(tf[qt])
		numerator := f * (e.k1 + 1)
		denominator := f + e.k1*(1-e.b+e.b*dl/e.avgdl)
		score += idfVal * numerator / denominator
	}
	return score
}
