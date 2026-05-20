import { Marked } from "marked";

const marked = new Marked({
  gfm: true,
  breaks: true,
});

const allowedTags = new Set([
  "a",
  "blockquote",
  "br",
  "code",
  "del",
  "em",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "hr",
  "input",
  "li",
  "ol",
  "p",
  "pre",
  "strong",
  "table",
  "tbody",
  "td",
  "th",
  "thead",
  "tr",
  "ul",
]);

const blockedTags = new Set([
  "embed",
  "form",
  "iframe",
  "link",
  "meta",
  "object",
  "script",
  "style",
]);

const allowedProtocols = new Set(["http:", "https:", "mailto:", "tel:"]);

export function renderMarkdown(content: string): string {
  const html = marked.parse(content) as string;
  return sanitizeHtml(html);
}

function sanitizeHtml(html: string): string {
  if (typeof document === "undefined") {
    return escapeHtml(html);
  }

  const template = document.createElement("template");
  template.innerHTML = html;

  const container = document.createElement("div");
  for (const node of Array.from(template.content.childNodes)) {
    const sanitizedNode = sanitizeNode(node);
    if (sanitizedNode) {
      container.appendChild(sanitizedNode);
    }
  }

  return container.innerHTML;
}

function sanitizeNode(node: Node): Node | DocumentFragment | null {
  if (node.nodeType === Node.TEXT_NODE) {
    return document.createTextNode(node.textContent ?? "");
  }

  if (node.nodeType !== Node.ELEMENT_NODE) {
    return null;
  }

  return sanitizeElement(node as HTMLElement);
}

function sanitizeElement(element: HTMLElement): Node | DocumentFragment | null {
  const tagName = element.tagName.toLowerCase();
  if (blockedTags.has(tagName)) {
    return null;
  }

  if (!allowedTags.has(tagName)) {
    return sanitizeChildren(element);
  }

  if (tagName === "input") {
    return sanitizeCheckbox(element);
  }

  const clone = document.createElement(tagName);
  if (tagName === "a") {
    applyAnchorAttributes(element, clone as HTMLAnchorElement);
  }

  for (const child of Array.from(element.childNodes)) {
    const sanitizedChild = sanitizeNode(child);
    if (sanitizedChild) {
      clone.appendChild(sanitizedChild);
    }
  }

  return clone;
}

function sanitizeChildren(element: HTMLElement): DocumentFragment {
  const fragment = document.createDocumentFragment();
  for (const child of Array.from(element.childNodes)) {
    const sanitizedChild = sanitizeNode(child);
    if (sanitizedChild) {
      fragment.appendChild(sanitizedChild);
    }
  }
  return fragment;
}

function sanitizeCheckbox(element: HTMLElement): HTMLInputElement | null {
  if (!(element instanceof HTMLInputElement) || element.type !== "checkbox") {
    return null;
  }

  const clone = document.createElement("input");
  clone.type = "checkbox";
  clone.disabled = true;
  clone.checked = element.checked;
  return clone;
}

function applyAnchorAttributes(source: HTMLElement, target: HTMLAnchorElement): void {
  const href = sanitizeUrl(source.getAttribute("href"));
  if (!href) {
    return;
  }

  target.setAttribute("href", href);
  const title = source.getAttribute("title");
  if (title) {
    target.setAttribute("title", title);
  }

  if (!isLocalHref(href)) {
    target.setAttribute("target", "_blank");
    target.setAttribute("rel", "noopener noreferrer");
  }
}

function sanitizeUrl(value: string | null): string | null {
  if (!value) {
    return null;
  }

  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }

  if (isLocalHref(trimmed)) {
    return trimmed;
  }

  try {
    const url = new URL(trimmed, window.location.origin);
    if (!allowedProtocols.has(url.protocol)) {
      return null;
    }
    return trimmed;
  } catch {
    return null;
  }
}

function isLocalHref(value: string): boolean {
  return value.startsWith("#") || value.startsWith("/") || value.startsWith("./") || value.startsWith("../");
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
