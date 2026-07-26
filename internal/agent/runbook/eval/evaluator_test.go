package eval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
)

func TestRAGEval(t *testing.T) {
	docs := []runbook.Document{
		{Title: "HighCPU", File: "high-cpu.md", ApplicableAlerts: []string{"HighCPU"}, Keywords: []string{"cpu", "load"}, Metrics: []string{"server_monitor_cpu_usage_percent"}, Body: "CPU 使用率在 15m 窗口内持续高于阈值"},
		{Title: "HighMemory", File: "high-memory.md", ApplicableAlerts: []string{"HighMemory"}, Keywords: []string{"memory", "mem"}, Metrics: []string{"server_monitor_memory_usage_percent"}, Body: "内存使用率持续升高"},
		{Title: "HighDisk", File: "high-disk.md", ApplicableAlerts: []string{"HighDisk"}, Keywords: []string{"disk", "filesystem"}, Metrics: []string{"server_monitor_disk_usage_percent"}, Body: "磁盘使用率持续高于阈值"},
	}
	retriever := runbook.NewRetriever(docs, runbook.RetrieverOptions{
		DefaultLimit: 3,
		MaxLimit:     5,
		BM25K1:       1.2,
		BM25B:        0.75,
	})

	cases := []RAGEvalCase{
		{Query: "HighCPU", WantFile: "high-cpu.md", Category: "precise", Description: "exact alert name"},
		{Query: "HighMemory", WantFile: "high-memory.md", Category: "precise", Description: "exact alert name"},
		{Query: "修改密码", WantFile: "", Category: "no_result", Description: "no match"},
	}

	result := EvaluateRAG(retriever, cases)

	if result.Total != 3 {
		t.Errorf("expected Total=3, got %d", result.Total)
	}
	if result.Top1Correct < 2 {
		t.Errorf("expected at least 2 correct, got %d", result.Top1Correct)
	}
	if _, ok := result.ByCategory["precise"]; !ok {
		t.Error("expected 'precise' category in results")
	}
	if _, ok := result.ByCategory["no_result"]; !ok {
		t.Error("expected 'no_result' category in results")
	}
}

func TestRAGEval_AccuracyThreshold(t *testing.T) {
	runbookDir := filepath.Join("..", "..", "..", "..", "runbooks")
	docs, err := runbook.LoadDir(context.Background(), runbookDir, runbook.LoadOptions{})
	if err != nil {
		t.Fatalf("load runbooks: %v", err)
	}
	if len(docs) == 0 {
		t.Skip("no runbook documents found")
	}

	retriever := runbook.NewRetriever(docs, runbook.RetrieverOptions{
		DefaultLimit: 5,
		MaxLimit:     5,
		BM25K1:       1.2,
		BM25B:        0.75,
	})

	cases := RAGEvalSet()
	result := EvaluateRAG(retriever, cases)

	t.Logf("RAG Eval: Total=%d, Top1Accuracy=%.1f%%", result.Total, result.Top1Accuracy*100)
	for cat, cr := range result.ByCategory {
		t.Logf("  %s: %d/%d (%.1f%%)", cat, cr.Correct, cr.Total, cr.Accuracy*100)
	}

	if result.Top1Accuracy < 0.95 {
		t.Errorf("RAG Top-1 accuracy %.1f%% is below threshold 95%%", result.Top1Accuracy*100)
	}

	preciseResult := result.ByCategory["precise"]
	if preciseResult.Total > 0 && preciseResult.Accuracy < 1.0 {
		t.Errorf("RAG precise category accuracy %.1f%% is below threshold 100%%", preciseResult.Accuracy*100)
	}
	keywordResult := result.ByCategory["keyword"]
	if keywordResult.Total > 0 && keywordResult.Accuracy < 0.85 {
		t.Errorf("RAG keyword category accuracy %.1f%% is below threshold 85%%", keywordResult.Accuracy*100)
	}
}
