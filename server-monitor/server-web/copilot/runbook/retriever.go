package runbook

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultSearchLimit = 2
	defaultMaxLimit    = 5
	maxSnippetRunes    = 800
)

type RAGObserver interface {
	ObserveRAGSearch(hasResult string, score, durationSeconds float64)
}

type Retriever struct {
	docs         []Document
	defaultLimit int
	maxLimit     int
	bm25         *BM25Engine
	bm25Weight   float64
	structWeight float64
	observer     RAGObserver
}

func NewRetriever(docs []Document, options RetrieverOptions) *Retriever {
	defaultLimit := options.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = defaultSearchLimit
	}
	maxLimit := options.MaxLimit
	if maxLimit <= 0 {
		maxLimit = defaultMaxLimit
	}
	if defaultLimit > maxLimit {
		defaultLimit = maxLimit
	}

	bm25Weight := options.BM25Weight
	if bm25Weight > 1 {
		bm25Weight = 1
	}
	structWeight := 1 - bm25Weight

	k1 := options.BM25K1
	if k1 <= 0 {
		k1 = 1.2
	}
	b := options.BM25B
	if b < 0 || b > 1 {
		b = 0.75
	}

	var bm25 *BM25Engine
	if bm25Weight > 0 {
		bm25 = NewBM25Engine(docs, k1, b)
	}

	return &Retriever{
		docs:         cloneDocuments(docs),
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
		bm25:         bm25,
		bm25Weight:   bm25Weight,
		structWeight: structWeight,
		observer:     options.Observer,
	}
}

func (r *Retriever) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrUnavailable
	}
	if len(r.docs) == 0 {
		return nil, ErrUnavailable
	}

	alertName := strings.TrimSpace(req.AlertName)
	keywords := normalizeTerms(req.Keywords)
	metrics := normalizeTerms(req.Metrics)
	limit := req.Limit
	if limit <= 0 {
		limit = r.defaultLimit
	}
	if limit > r.maxLimit {
		limit = r.maxLimit
	}

	queryTokens := buildQueryTokens(alertName, keywords, metrics)

	type candidate struct {
		docIdx          int
		rawStructScore  float64
		rawBM25Score    float64
		matchedAlerts   []string
		matchedKeywords []string
		matchedMetrics  []string
	}

	candidates := make([]candidate, 0, len(r.docs))
	var maxStructScore float64
	var maxBM25Score float64

	for i, doc := range r.docs {
		rawStructScore, matchedAlerts, matchedKeywords, matchedMetrics := scoreDocument(doc, alertName, keywords, metrics)
		var rawBM25Score float64
		if r.bm25 != nil {
			rawBM25Score = r.bm25.Score(queryTokens, i)
		}
		if rawStructScore <= 0 && rawBM25Score <= 0 {
			continue
		}
		if rawStructScore > maxStructScore {
			maxStructScore = rawStructScore
		}
		if rawBM25Score > maxBM25Score {
			maxBM25Score = rawBM25Score
		}
		candidates = append(candidates, candidate{
			docIdx:          i,
			rawStructScore:  rawStructScore,
			rawBM25Score:    rawBM25Score,
			matchedAlerts:   matchedAlerts,
			matchedKeywords: matchedKeywords,
			matchedMetrics:  matchedMetrics,
		})
	}

	results := make([]SearchResult, 0, len(candidates))
	for _, c := range candidates {
		structNorm := 0.0
		if maxStructScore > 0 {
			structNorm = c.rawStructScore / maxStructScore
		}
		bm25Norm := 0.0
		if maxBM25Score > 0 {
			bm25Norm = c.rawBM25Score / maxBM25Score
		}
		finalScore := structNorm*r.structWeight + bm25Norm*r.bm25Weight
		if finalScore <= 0 {
			continue
		}
		doc := r.docs[c.docIdx]
		results = append(results, SearchResult{
			Title:           doc.Title,
			File:            doc.File,
			Score:           finalScore,
			MatchedAlerts:   c.matchedAlerts,
			MatchedKeywords: c.matchedKeywords,
			MatchedMetrics:  c.matchedMetrics,
			Snippet:         snippetFor(doc, alertName, keywords, metrics),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Title < results[j].Title
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}

	if r.observer != nil {
		hasResult := "false"
		topScore := 0.0
		if len(results) > 0 {
			hasResult = "true"
			topScore = results[0].Score
		}
		r.observer.ObserveRAGSearch(hasResult, topScore, time.Since(start).Seconds())
	}

	return results, nil
}

func (r *Retriever) HealthCheck(ctx context.Context) bool {
	return ctx.Err() == nil && r != nil && len(r.docs) > 0
}

func (r *Retriever) Count() int {
	if r == nil {
		return 0
	}
	return len(r.docs)
}

func buildQueryTokens(alertName string, keywords, metrics []string) []string {
	seen := make(map[string]struct{})
	var tokens []string
	addTokens := func(text string) {
		for _, t := range Tokenize(text) {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				tokens = append(tokens, t)
			}
		}
	}
	if alertName != "" {
		addTokens(alertName)
	}
	for _, kw := range keywords {
		addTokens(kw)
	}
	for _, m := range metrics {
		addTokens(m)
	}
	return tokens
}

func scoreDocument(doc Document, alertName string, keywords, metrics []string) (float64, []string, []string, []string) {
	score := 0.0
	body := strings.ToLower(doc.Body)
	title := strings.ToLower(doc.Title)
	alertLower := strings.ToLower(strings.TrimSpace(alertName))
	matchedAlerts := []string{}
	matchedKeywords := []string{}
	matchedMetrics := []string{}

	if alertLower != "" {
		if strings.EqualFold(doc.Title, alertName) || containsFold(doc.ApplicableAlerts, alertName) {
			score += 10
			matchedAlerts = append(matchedAlerts, alertName)
		} else if strings.Contains(body, alertLower) {
			score += 3
			matchedAlerts = append(matchedAlerts, alertName)
		}
	}
	for _, keyword := range keywords {
		keywordLower := strings.ToLower(keyword)
		if containsFold(doc.Keywords, keyword) {
			score += 2
			matchedKeywords = append(matchedKeywords, keyword)
		} else if strings.Contains(body, keywordLower) {
			score += 1
			matchedKeywords = append(matchedKeywords, keyword)
		}
		if strings.Contains(title, keywordLower) {
			score += 3
		}
	}
	for _, metric := range metrics {
		metricLower := strings.ToLower(metric)
		if containsFold(doc.Metrics, metric) {
			score += 5
			matchedMetrics = append(matchedMetrics, metric)
		} else if strings.Contains(body, metricLower) {
			score += 2
			matchedMetrics = append(matchedMetrics, metric)
		}
	}
	return score, compactFold(matchedAlerts), compactFold(matchedKeywords), compactFold(matchedMetrics)
}

func snippetFor(doc Document, alertName string, keywords, metrics []string) string {
	terms := append([]string{alertName}, keywords...)
	terms = append(terms, metrics...)
	for _, section := range doc.Sections {
		text := strings.TrimSpace(section.Text)
		if text == "" {
			continue
		}
		sectionText := strings.ToLower(section.Heading + "\n" + text)
		for _, term := range terms {
			term = strings.ToLower(strings.TrimSpace(term))
			if term != "" && strings.Contains(sectionText, term) {
				return truncateRunes(collapseBlankLines(text), maxSnippetRunes)
			}
		}
	}
	return truncateRunes(collapseBlankLines(doc.Body), 500)
}

func normalizeTerms(values []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func compactFold(values []string) []string {
	return normalizeTerms(values)
}

func collapseBlankLines(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank && prevBlank {
			continue
		}
		out = append(out, line)
		prevBlank = blank
	}
	return strings.Join(out, "\n")
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	out := make([]rune, 0, max)
	for _, r := range value {
		if len(out) >= max {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func cloneDocuments(docs []Document) []Document {
	cloned := make([]Document, len(docs))
	for i, doc := range docs {
		cloned[i] = doc
		cloned[i].ApplicableAlerts = append([]string(nil), doc.ApplicableAlerts...)
		cloned[i].Keywords = append([]string(nil), doc.Keywords...)
		cloned[i].Metrics = append([]string(nil), doc.Metrics...)
		cloned[i].Sections = append([]Section(nil), doc.Sections...)
	}
	return cloned
}
