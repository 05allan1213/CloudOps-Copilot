import { describe, expect, it } from "vitest";

import { renderAgentMarkdown } from "./agentMarkdown";

describe("Agent Markdown renderer", () => {
  it("renders operational Markdown as a document canvas", () => {
    const html = renderAgentMarkdown(`# 调查结论

- **状态**：已定位
- 资源：\`Deployment/checkout-api\`

| 检查 | 结果 |
| --- | --- |
| Readiness | 通过 |

\`\`\`yaml
replicas: 3
\`\`\``);

    expect(html).toContain("<h1>调查结论</h1>");
    expect(html).toContain("<ul>");
    expect(html).toContain("<strong>状态</strong>");
    expect(html).toContain("<table>");
    expect(html).toContain("language-yaml");
  });

  it("escapes raw HTML, omits images, and drops unsafe link protocols", () => {
    const html = renderAgentMarkdown(`<script>alert(1)</script>

[危险链接](javascript:alert(1))

![外部图片](https://example.com/secret.png)

[Evidence](https://example.com/evidence)`);

    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
    expect(html).not.toContain("javascript:");
    expect(html).not.toContain("<img");
    expect(html).toContain("外部图片");
    expect(html).toContain("rel=\"noopener noreferrer\"");
  });
});
