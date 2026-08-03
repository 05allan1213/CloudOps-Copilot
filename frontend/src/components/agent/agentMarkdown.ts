import { Marked, type Tokens } from "marked";

const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;",
  })[character] ?? character);
}

function safeHref(href: string): string | null {
  const value = href.trim();
  if (!value) return null;
  try {
    const parsed = new URL(value, "https://cloudops.local");
    return SAFE_LINK_PROTOCOLS.has(parsed.protocol) ? value : null;
  } catch {
    return null;
  }
}

const markdown = new Marked({
  gfm: true,
  breaks: true,
  renderer: {
    html(token: Tokens.HTML | Tokens.Tag) {
      return escapeHTML(token.text);
    },
    image(token: Tokens.Image) {
      return escapeHTML(token.text);
    },
    link(token: Tokens.Link) {
      const label = this.parser.parseInline(token.tokens);
      const href = safeHref(token.href);
      if (!href) return label;
      const title = token.title ? ` title="${escapeHTML(token.title)}"` : "";
      const external = /^https?:/i.test(href) ? " target=\"_blank\" rel=\"noopener noreferrer\"" : "";
      return `<a href="${escapeHTML(href)}"${title}${external}>${label}</a>`;
    },
  },
});

export function renderAgentMarkdown(source: string): string {
  return markdown.parse(source, { async: false });
}
