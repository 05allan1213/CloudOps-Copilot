import { expect, type APIRequestContext, type Page } from "@playwright/test";

export const fixtureOrigin = "http://127.0.0.1:18082";
export const incidentID = "00000000-0000-4000-8000-000000000001";

export interface FixtureOptions {
  command?: "202" | "403" | "409" | "422" | "501" | "503" | "timeout";
  plan?: "valid" | "expired" | "stale";
  verification?: "passed" | "failed" | "timed_out" | "inconclusive" | "not_run" | "no_change";
  list?: "ready" | "loading" | "timeout" | "empty" | "error" | "forbidden" | "one" | "twenty" | "fifty" | "long" | "paginated";
  detail?: "ready" | "loading" | "error" | "forbidden";
  sections?: "ready" | "empty" | "error" | "states" | "paged";
  sse?: "connected" | "reconnect" | "finite" | "offline";
}

export async function configureFixture(request: APIRequestContext, options: FixtureOptions = {}) {
  const params = new URLSearchParams({ reset: "1" });
  for (const [key, value] of Object.entries(options)) {
    if (value !== undefined) params.set(key, value);
  }
  const response = await request.get(`${fixtureOrigin}/fixture/config?${params}`);
  expect(response.ok()).toBeTruthy();
  return response.json();
}

export function monitorBrowser(page: Page, options: { allowedFailures?: RegExp[] } = {}) {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];
  const failedRequests: string[] = [];
  const failedResponses: string[] = [];
  const unexpected = (messages: string[]) => messages.filter((message) => !options.allowedFailures?.some((pattern) => {
    pattern.lastIndex = 0;
    return pattern.test(message);
  }));
  page.on("console", (message) => {
    if (message.type() === "error" || message.type() === "warning") {
      const location = message.location().url;
      consoleErrors.push(`${message.text()}${location ? ` @ ${location}` : ""}`);
    }
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));
  page.on("requestfailed", (request) => failedRequests.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText || "failed"}`));
  page.on("response", (response) => {
    if (response.status() >= 400) failedResponses.push(`${response.status()} ${response.url()}`);
  });
  return {
    consoleErrors,
    pageErrors,
    failedRequests,
    failedResponses,
    expectClean() {
      expect({
        consoleErrors: unexpected(consoleErrors),
        pageErrors: unexpected(pageErrors),
        failedRequests: unexpected(failedRequests),
        failedResponses: unexpected(failedResponses),
      }).toEqual({
        consoleErrors: [],
        pageErrors: [],
        failedRequests: [],
        failedResponses: [],
      });
    },
  };
}

export async function expectNoLayoutOverflow(page: Page) {
  const result = await page.evaluate(() => {
    const allowedContainers = ".desktop-table-wrap, .diff-scroll, .attempt-history > div, .sample-history pre, .drawer-body, .dialog-body, .json-snapshot pre";
    const describe = (element: Element) => {
      const id = element.id ? `#${element.id}` : "";
      const classes = typeof element.className === "string"
        ? `.${element.className.split(/\s+/).filter(Boolean).slice(0, 2).join(".")}`
        : "";
      return `${element.tagName.toLowerCase()}${id}${classes}`;
    };
    const overflow = [...document.querySelectorAll("*")].filter((element) => {
      if (!(element instanceof HTMLElement)
        || element.clientWidth === 0
        || element.classList.contains("visually-hidden")
        || element.closest("dialog:not([open])")
        || element.matches(allowedContainers)
        || element.closest(allowedContainers)) return false;
      if (element.scrollWidth <= element.clientWidth + 1) return false;
      const style = getComputedStyle(element);
      if (style.textOverflow === "ellipsis" && ["hidden", "clip"].includes(style.overflowX)) return false;
      return style.overflowX !== "auto" && style.overflowX !== "scroll";
    }).slice(0, 30).map(describe);
    const main = document.querySelector<HTMLElement>(".app-main");
    return {
      documentWidth: [document.documentElement.scrollWidth, document.documentElement.clientWidth],
      mainWidth: main ? [main.scrollWidth, main.clientWidth] : [],
      overflow,
    };
  });
  expect(result.documentWidth[0]).toBe(result.documentWidth[1]);
  expect(result.mainWidth[0]).toBe(result.mainWidth[1]);
  expect(result.overflow).toEqual([]);
}

export async function expectMinimumTarget(page: Page, selector: string, minimum = 44) {
  const targets = page.locator(selector);
  await expect(targets.first()).toBeVisible();
  const sizes = await targets.evaluateAll((elements) => elements
    .filter((element) => {
      const style = getComputedStyle(element);
      return style.display !== "none" && style.visibility !== "hidden";
    })
    .map((element) => {
      const rect = element.getBoundingClientRect();
      return { width: rect.width, height: rect.height };
    }));
  for (const size of sizes) {
    expect(size.width).toBeGreaterThanOrEqual(minimum);
    expect(size.height).toBeGreaterThanOrEqual(minimum);
  }
}

export async function expectHeadingOrder(page: Page) {
  const levels = await page.locator("main h1, main h2, main h3, main h4, main h5, main h6").evaluateAll((headings) => headings
    .filter((heading) => (heading as HTMLElement).checkVisibility())
    .map((heading) => Number(heading.tagName.slice(1))));
  expect(levels[0]).toBe(1);
  for (let index = 1; index < levels.length; index += 1) {
    expect(levels[index] - levels[index - 1]).toBeLessThanOrEqual(1);
  }
}
