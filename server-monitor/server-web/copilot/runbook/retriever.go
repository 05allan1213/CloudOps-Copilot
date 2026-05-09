package runbook

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	defaultSearchLimit = 2
	defaultMaxLimit    = 5
	maxSnippetRunes    = 800
)

type Retriever struct {
	docs         []Document
	defaultLimit int
	maxLimit     int
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
	return &Retriever{
		docs:         cloneDocuments(docs),
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
	}
}

func (r *Retriever) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
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

	results := make([]SearchResult, 0, len(r.docs))
	for _, doc := range r.docs {
		score, matchedAlerts, matchedKeywords, matchedMetrics := scoreDocument(doc, alertName, keywords, metrics)
		if score <= 0 {
			continue
		}
		results = append(results, SearchResult{
			Title:           doc.Title,
			File:            doc.File,
			Score:           score,
			MatchedAlerts:   matchedAlerts,
			MatchedKeywords: matchedKeywords,
			MatchedMetrics:  matchedMetrics,
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
