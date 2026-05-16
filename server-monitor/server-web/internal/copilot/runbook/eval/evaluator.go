package eval

import (
	"context"
	"encoding/json"

	"server-web/internal/copilot/runbook"
)

type RAGEvalResult struct {
	Total        int
	Top1Correct  int
	Top1Accuracy float64
	ByCategory   map[string]CategoryResult
}

type CategoryResult struct {
	Total    int
	Correct  int
	Accuracy float64
}

func EvaluateRAG(retriever *runbook.Retriever, cases []RAGEvalCase) RAGEvalResult {
	result := RAGEvalResult{
		ByCategory: make(map[string]CategoryResult),
	}

	for _, c := range cases {
		cat := result.ByCategory[c.Category]
		cat.Total++
		result.Total++

		req := runbook.SearchRequest{
			Keywords: []string{c.Query},
		}
		if c.Category == "precise" {
			req.AlertName = c.Query
		}

		results, _ := retriever.Search(context.Background(), req)

		correct := false
		if c.Category == "no_result" {
			if len(results) == 0 {
				correct = true
			} else {
				top := results[0]
				hasStructMatch := len(top.MatchedAlerts) > 0 || len(top.MatchedKeywords) > 0 || len(top.MatchedMetrics) > 0
				correct = !hasStructMatch
			}
		} else {
			correct = len(results) > 0 && results[0].File == c.WantFile
		}

		if correct {
			cat.Correct++
			result.Top1Correct++
		}

		result.ByCategory[c.Category] = cat
	}

	if result.Total > 0 {
		result.Top1Accuracy = float64(result.Top1Correct) / float64(result.Total)
	}

	for name, cat := range result.ByCategory {
		if cat.Total > 0 {
			cat.Accuracy = float64(cat.Correct) / float64(cat.Total)
		}
		result.ByCategory[name] = cat
	}

	return result
}

func (r RAGEvalResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

func RAGEvalResultFromJSON(data []byte) (RAGEvalResult, error) {
	var r RAGEvalResult
	return r, json.Unmarshal(data, &r)
}
