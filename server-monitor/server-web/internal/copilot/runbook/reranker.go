package runbook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type LLMGenerator interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type Reranker struct {
	llm      LLMGenerator
	topN     int
	timeout  time.Duration
	maxInput int
}

type RerankerOptions struct {
	LLM      LLMGenerator
	TopN     int
	Timeout  time.Duration
	MaxInput int
}

func NewReranker(opts RerankerOptions) *Reranker {
	if opts.LLM == nil {
		return nil
	}
	topN := opts.TopN
	if topN <= 0 {
		topN = 2
	}
	if topN > 5 {
		topN = 5
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout < time.Second {
		timeout = time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	maxInput := opts.MaxInput
	if maxInput <= 0 {
		maxInput = 10
	}
	if maxInput > 20 {
		maxInput = 20
	}
	return &Reranker{
		llm:      opts.LLM,
		topN:     topN,
		timeout:  timeout,
		maxInput: maxInput,
	}
}

func (r *Reranker) Rerank(ctx context.Context, query string, candidates []SearchResult) ([]SearchResult, error) {
	if r == nil || r.llm == nil {
		return candidates, nil
	}
	if len(candidates) == 0 {
		return []SearchResult{}, nil
	}

	input := candidates
	if len(input) > r.maxInput {
		input = input[:r.maxInput]
	}

	systemPrompt := strings.Join([]string{
		"You are a runbook relevance ranker. Given a query and a list of runbook candidates, rank them by relevance to the query.",
		"Return a JSON array of objects with \"file\" and \"reason\" fields, ordered from most relevant to least relevant.",
		"Only include candidates that are relevant to the query.",
		`Example: [{"file": "high-cpu.md", "reason": "Directly addresses CPU issues"}]`,
	}, "\n")

	var sb strings.Builder
	sb.WriteString("Query: ")
	sb.WriteString(query)
	sb.WriteString("\n\nCandidates:\n")
	for i, c := range input {
		sb.WriteString(fmt.Sprintf("%d. Title: %s, File: %s, Score: %.2f\n", i+1, c.Title, c.File, c.Score))
		sb.WriteString("   Snippet: ")
		sb.WriteString(c.Snippet)
		sb.WriteByte('\n')
	}
	userPrompt := sb.String()

	rerankCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	content, err := r.llm.Generate(rerankCtx, systemPrompt, userPrompt)
	if err != nil {
		return candidates, nil
	}

	var ranking []struct {
		File   string `json:"file"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &ranking); err != nil {
		return candidates, nil
	}

	fileMap := make(map[string]SearchResult, len(candidates))
	for _, c := range candidates {
		fileMap[c.File] = c
	}

	var results []SearchResult
	for _, item := range ranking {
		if c, ok := fileMap[item.File]; ok {
			results = append(results, c)
		}
	}
	if len(results) == 0 {
		return candidates, nil
	}
	if len(results) > r.topN {
		results = results[:r.topN]
	}
	return results, nil
}
