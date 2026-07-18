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
		{Query: "HostDown", WantFile: "host-down.md", Category: "precise", Description: "exact alert name match"},

		{Query: "CPU飙高", WantFile: "high-cpu.md", Category: "fuzzy", Description: "Chinese fuzzy CPU"},
		{Query: "内存告警", WantFile: "high-memory.md", Category: "fuzzy", Description: "Chinese fuzzy memory"},
		{Query: "磁盘满了", WantFile: "high-disk.md", Category: "fuzzy", Description: "Chinese fuzzy disk"},
		{Query: "主机下线", WantFile: "host-down.md", Category: "fuzzy", Description: "Chinese fuzzy host down"},
		{Query: "CPU严重", WantFile: "critical-cpu.md", Category: "fuzzy", Description: "Chinese fuzzy critical CPU"},

		{Query: "服务器卡住了", WantFile: "high-cpu.md", Category: "semantic", Description: "semantic: server stuck"},
		{Query: "系统变慢了", WantFile: "high-cpu.md", Category: "semantic", Description: "semantic: system slow"},
		{Query: "磁盘满了怎么办", WantFile: "high-disk.md", Category: "semantic", Description: "semantic: disk full what to do"},
		{Query: "内存不够用了", WantFile: "high-memory.md", Category: "semantic", Description: "semantic: memory running out"},
		{Query: "机器挂了", WantFile: "host-down.md", Category: "semantic", Description: "semantic: machine dead"},
		{Query: "CPU占用太高", WantFile: "high-cpu.md", Category: "semantic", Description: "semantic: CPU usage too high"},
		{Query: "存储空间不足", WantFile: "high-disk.md", Category: "semantic", Description: "semantic: storage insufficient"},
		{Query: "进程占用内存过大", WantFile: "high-memory.md", Category: "semantic", Description: "semantic: process using too much memory"},

		{Query: "CPU和内存都高", WantFile: "high-cpu.md", Category: "ambiguous", Description: "ambiguous: both CPU and memory high"},
		{Query: "资源使用率过高", WantFile: "high-cpu.md", Category: "ambiguous", Description: "ambiguous: resource usage too high"},
		{Query: "系统性能下降", WantFile: "high-cpu.md", Category: "ambiguous", Description: "ambiguous: system performance degraded"},
		{Query: "服务器异常", WantFile: "host-down.md", Category: "ambiguous", Description: "ambiguous: server abnormal"},

		{Query: "修改密码", WantFile: "", Category: "no_result", Description: "no match: change password"},
		{Query: "如何安装软件", WantFile: "", Category: "no_result", Description: "no match: how to install software"},
		{Query: "网络配置", WantFile: "", Category: "no_result", Description: "no match: network config"},
	}
}
