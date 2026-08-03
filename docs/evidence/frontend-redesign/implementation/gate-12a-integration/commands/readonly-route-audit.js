async (page) => {
  const baseURL = "http://127.0.0.1:5173";
  const screenshotRoot = "/home/monody/k8s/CloudOps-Copilot/output/playwright/gate-12a/browser";
  const allRoutes = [
    { key: "root-redirect", path: "/" },
    { key: "overview", path: "/overview" },
    { key: "atlas", path: "/atlas" },
    { key: "atlas-structured", path: "/atlas?view=structured" },
    { key: "infrastructure", path: "/infrastructure" },
    { key: "monitoring", path: "/monitoring?execution=3d643a53-ecea-4221-9b78-e53fd745bf15" },
    { key: "alerts", path: "/alerts" },
    { key: "alert-detail", path: "/alerts/34035d63-8932-11f1-b946-126b53222cff" },
    { key: "logs", path: "/logs?query=f020b5a6-7723-4ac6-978b-8c759ba6d7b1" },
    { key: "traces", path: "/traces?search=7e1af181-aa7d-4a18-829a-affb71b94be4" },
    { key: "agent", path: "/agent?investigation=78039742-0653-4e6e-9541-54ef93e1e9ca" },
    { key: "incidents", path: "/incidents?selected=21c123ac-7199-4dff-a64b-7384f3550ea3" },
    { key: "incident-detail", path: "/incidents/21c123ac-7199-4dff-a64b-7384f3550ea3" },
    { key: "devops", path: "/devops" },
    { key: "settings", path: "/settings" },
    { key: "not-found", path: "/gate-12a-not-found" },
  ];
  const requestedKeys = Array.isArray(page.__g12aAuditRouteKeys) ? page.__g12aAuditRouteKeys : [];
  const compact = page.__g12aAuditCompact === true;
  const routes = requestedKeys.length
    ? allRoutes.filter((routeCase) => requestedKeys.includes(routeCase.key))
    : allRoutes;
  page.__g12aAuditRouteKeys = undefined;
  page.__g12aAuditCompact = undefined;

  await page.unroute("**/*");
  const blockedWrites = [];
  const allowedMethods = new Set(["GET", "HEAD", "OPTIONS"]);
  await page.route("**/*", async (route) => {
    const request = route.request();
    const method = request.method().toUpperCase();
    if (!allowedMethods.has(method)) {
      blockedWrites.push({ method, url: request.url(), resourceType: request.resourceType() });
      await route.abort("blockedbyclient");
      return;
    }
    await route.continue();
  });

  const results = [];
  for (const routeCase of routes) {
    const consoleMessages = [];
    const pageErrors = [];
    const failedRequests = [];
    const apiResponses = [];
    const pendingResponses = [];
    const blockedStart = blockedWrites.length;

    const onConsole = (message) => {
      if (message.type() === "error" || message.type() === "warning") {
        consoleMessages.push({ type: message.type(), text: message.text() });
      }
    };
    const onPageError = (error) => pageErrors.push(error.message);
    const onRequestFailed = (request) => {
      failedRequests.push({
        method: request.method(),
        url: request.url(),
        error: request.failure()?.errorText || "unknown",
      });
    };
    const onResponse = (response) => {
      const url = response.url();
      if (!url.startsWith(`${baseURL}/api/`)) return;
      pendingResponses.push((async () => {
        const headers = await response.allHeaders();
        apiResponses.push({
          method: response.request().method(),
          status: response.status(),
          url: url.replace(baseURL, ""),
          requestID: headers["x-request-id"] || "",
          traceID: headers["x-trace-id"] || headers.traceparent || "",
        });
      })());
    };

    page.on("console", onConsole);
    page.on("pageerror", onPageError);
    page.on("requestfailed", onRequestFailed);
    page.on("response", onResponse);

    let navigationStatus = 0;
    let navigationError = "";
    try {
      const response = await page.goto(`${baseURL}${routeCase.path}`, {
        waitUntil: "domcontentloaded",
        timeout: 20_000,
      });
      navigationStatus = response?.status() || 0;
      await page.locator("h1").first().waitFor({ state: "visible", timeout: 10_000 });
      await page.waitForFunction(
        () => document.querySelectorAll(".workspace-state-loading").length === 0,
        { timeout: 20_000 },
      );
      await page.waitForTimeout(500);
    } catch (error) {
      navigationError = error instanceof Error ? error.message : String(error);
    }

    await Promise.allSettled(pendingResponses);
    const pageState = await page.evaluate(() => {
      const visible = (element) => {
        const style = window.getComputedStyle(element);
        const rect = element.getBoundingClientRect();
        return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
      };
      const describe = (element) => {
        const text = (element.getAttribute("aria-label") || element.textContent || "")
          .replace(/\s+/g, " ")
          .trim()
          .slice(0, 100);
        return `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ""}${text ? `:${text}` : ""}`;
      };
      const clippedRect = (element) => {
        const source = element.getBoundingClientRect();
        let left = Math.max(0, source.left);
        let right = Math.min(window.innerWidth, source.right);
        let top = Math.max(0, source.top);
        let bottom = Math.min(window.innerHeight, source.bottom);
        let current = element.parentElement;
        while (current && current !== document.body) {
          const style = window.getComputedStyle(current);
          const bounds = current.getBoundingClientRect();
          if (["auto", "scroll", "hidden", "clip"].includes(style.overflowX)) {
            left = Math.max(left, bounds.left);
            right = Math.min(right, bounds.right);
          }
          if (["auto", "scroll", "hidden", "clip"].includes(style.overflowY)) {
            top = Math.max(top, bounds.top);
            bottom = Math.min(bottom, bounds.bottom);
          }
          current = current.parentElement;
        }
        return {
          left,
          right,
          top,
          bottom,
          width: Math.max(0, right - left),
          height: Math.max(0, bottom - top),
        };
      };
      const overflow = [...document.body.querySelectorAll("*")]
        .filter((element) => visible(element))
        .map((element) => ({ element, rect: element.getBoundingClientRect() }))
        .filter(({ element, rect }) => {
          const style = window.getComputedStyle(element);
          if (style.position === "fixed") return false;
          return rect.left < -1 || rect.right > window.innerWidth + 1;
        })
        .slice(0, 12)
        .map(({ element, rect }) => ({
          element: describe(element),
          left: Math.round(rect.left),
          right: Math.round(rect.right),
          width: Math.round(rect.width),
          boundedByLocalScroll: (() => {
            let current = element.parentElement;
            while (current && current !== document.body) {
              const style = window.getComputedStyle(current);
              if (["auto", "scroll"].includes(style.overflowX) && current.scrollWidth > current.clientWidth) return true;
              current = current.parentElement;
            }
            return false;
          })(),
        }));
      const interactives = [...document.querySelectorAll("a[href], button, input, select, textarea, [role=button]")]
        .filter((element) => visible(element))
        .map((element) => ({ element, rect: clippedRect(element) }))
        .filter(({ rect }) => rect.width > 0 && rect.height > 0);
      const overlaps = [];
      for (let index = 0; index < interactives.length; index += 1) {
        for (let otherIndex = index + 1; otherIndex < interactives.length; otherIndex += 1) {
          const first = interactives[index];
          const second = interactives[otherIndex];
          if (first.element.contains(second.element) || second.element.contains(first.element)) continue;
          const width = Math.min(first.rect.right, second.rect.right) - Math.max(first.rect.left, second.rect.left);
          const height = Math.min(first.rect.bottom, second.rect.bottom) - Math.max(first.rect.top, second.rect.top);
          if (width <= 1 || height <= 1) continue;
          const intersection = width * height;
          const smaller = Math.min(first.rect.width * first.rect.height, second.rect.width * second.rect.height);
          if (smaller > 0 && intersection / smaller >= 0.2) {
            overlaps.push([describe(first.element), describe(second.element)]);
          }
          if (overlaps.length >= 12) break;
        }
        if (overlaps.length >= 12) break;
      }
      const main = document.querySelector("main");
      const bodyText = (main?.textContent || document.body.textContent || "").replace(/\s+/g, " ").trim();
      return {
        title: document.title,
        h1: document.querySelector("h1")?.textContent?.trim() || "",
        bodyTextLength: bodyText.length,
        bodyTextSample: bodyText.slice(0, 240),
        viewport: { width: window.innerWidth, height: window.innerHeight },
        documentWidth: document.documentElement.scrollWidth,
        documentHeight: document.documentElement.scrollHeight,
        mainWidth: main ? Math.round(main.getBoundingClientRect().width) : 0,
        horizontalOverflow: overflow,
        interactiveOverlaps: overlaps,
      };
    });

    await page.screenshot({ path: `${screenshotRoot}/${routeCase.key}.png`, fullPage: false });

    page.off("console", onConsole);
    page.off("pageerror", onPageError);
    page.off("requestfailed", onRequestFailed);
    page.off("response", onResponse);

    const uniqueResponses = [...new Map(apiResponses.map((item) => [
      `${item.method} ${item.status} ${item.url}`,
      item,
    ])).values()];
    results.push({
      key: routeCase.key,
      requestedPath: routeCase.path,
      finalURL: page.url(),
      navigationStatus,
      navigationError,
      ...pageState,
      apiResponses: uniqueResponses,
      consoleMessages,
      pageErrors,
      failedRequests,
      blockedWrites: blockedWrites.slice(blockedStart),
    });
  }

  const report = {
    browser: "Chromium",
    viewport: "1440x900",
    colorScheme: "light",
    allowedMethods: [...allowedMethods],
    blockedWrites,
    routes: results,
  };
  if (!compact) return report;
  return {
    ...report,
    routes: results.map((route) => ({
      key: route.key,
      requestedPath: route.requestedPath,
      finalURL: route.finalURL,
      navigationStatus: route.navigationStatus,
      navigationError: route.navigationError,
      h1: route.h1,
      bodyTextLength: route.bodyTextLength,
      documentWidth: route.documentWidth,
      unboundedOverflow: route.horizontalOverflow.filter((item) => !item.boundedByLocalScroll),
      interactiveOverlapCount: route.interactiveOverlaps.length,
      apiResponseCount: route.apiResponses.length,
      apiStatuses: [...new Set(route.apiResponses.map((item) => item.status))],
      apiPaths: route.apiResponses.slice(0, 8).map((item) => item.url),
      consoleMessages: route.consoleMessages,
      pageErrors: route.pageErrors,
      failedRequests: route.failedRequests,
      blockedWrites: route.blockedWrites,
    })),
  };
}
