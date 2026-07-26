package eval

type RAGEvalCase struct {
	Query       string
	WantFile    string
	Category    string
	Description string
}

func RAGEvalSet() []RAGEvalCase {
	return []RAGEvalCase{
		{Query: "HighCPU", WantFile: "high-cpu.md", Category: "precise", Description: "exact alert name match"},
		{Query: "HighMemory", WantFile: "high-memory.md", Category: "precise", Description: "exact alert name match"},
		{Query: "HighDisk", WantFile: "high-disk.md", Category: "precise", Description: "exact alert name match"},
		{Query: "CriticalCPU", WantFile: "critical-cpu.md", Category: "precise", Description: "exact alert name match"},
		{Query: "HighErrorRate", WantFile: "high-error-rate.md", Category: "precise", Description: "exact alert name match"},
		{Query: "HighLatency", WantFile: "high-latency.md", Category: "precise", Description: "exact alert name match"},
		{Query: "K8sDeploymentUnavailable", WantFile: "k8s-deployment-unavailable.md", Category: "precise", Description: "exact alert name match"},
		{Query: "K8sPodCrashLoopBackOff", WantFile: "k8s-pod-crashloopbackoff.md", Category: "precise", Description: "exact alert name match"},

		{Query: "critical", WantFile: "critical-cpu.md", Category: "keyword", Description: "structured keyword match"},
		{Query: "memory_available_bytes", WantFile: "high-memory.md", Category: "keyword", Description: "structured metric fragment match"},
		{Query: "mountpoint", WantFile: "high-disk.md", Category: "keyword", Description: "structured keyword match"},
		{Query: "5xx", WantFile: "high-error-rate.md", Category: "keyword", Description: "structured keyword match"},
		{Query: "p99", WantFile: "high-latency.md", Category: "keyword", Description: "structured keyword match"},
		{Query: "unavailable", WantFile: "k8s-deployment-unavailable.md", Category: "keyword", Description: "structured keyword match"},
		{Query: "crashloopbackoff", WantFile: "k8s-pod-crashloopbackoff.md", Category: "keyword", Description: "structured keyword match"},

		{Query: "修改密码", WantFile: "", Category: "no_result", Description: "no match: change password"},
		{Query: "如何安装软件", WantFile: "", Category: "no_result", Description: "no match: how to install software"},
		{Query: "网络配置", WantFile: "", Category: "no_result", Description: "no match: network config"},
	}
}
