package runbook

import "sort"

type RankItem struct {
	DocIdx          int
	Score           float64
	MatchedAlerts   []string
	MatchedKeywords []string
	MatchedMetrics  []string
}

type weightedRanking struct {
	weight float64
	items  []RankItem
}

func RRF(rankings [][]RankItem, k int) []RankItem {
	weighted := make([]weightedRanking, 0, len(rankings))
	for _, ranking := range rankings {
		weighted = append(weighted, weightedRanking{
			weight: 1,
			items:  ranking,
		})
	}
	return rrfWeighted(weighted, k)
}

func rrfWeighted(rankings []weightedRanking, k int) []RankItem {
	if k <= 0 {
		k = 60
	}

	type accum struct {
		rrfScore        float64
		matchedAlerts   []string
		matchedKeywords []string
		matchedMetrics  []string
	}

	m := make(map[int]*accum)

	for _, ranking := range rankings {
		if ranking.weight <= 0 || len(ranking.items) == 0 {
			continue
		}

		sorted := make([]RankItem, len(ranking.items))
		copy(sorted, ranking.items)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Score > sorted[j].Score
		})

		for rank, item := range sorted {
			a, ok := m[item.DocIdx]
			if !ok {
				a = &accum{
					matchedAlerts:   item.MatchedAlerts,
					matchedKeywords: item.MatchedKeywords,
					matchedMetrics:  item.MatchedMetrics,
				}
				m[item.DocIdx] = a
			}
			a.rrfScore += ranking.weight / float64(k+rank+1)
		}
	}

	if len(m) == 0 {
		return nil
	}

	result := make([]RankItem, 0, len(m))
	for docIdx, a := range m {
		result = append(result, RankItem{
			DocIdx:          docIdx,
			Score:           a.rrfScore,
			MatchedAlerts:   a.matchedAlerts,
			MatchedKeywords: a.matchedKeywords,
			MatchedMetrics:  a.matchedMetrics,
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].DocIdx < result[j].DocIdx
	})

	return result
}
