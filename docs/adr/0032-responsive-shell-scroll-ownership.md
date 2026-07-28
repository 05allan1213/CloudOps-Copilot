# ADR 0032: Responsive Shell and Scroll Ownership

- Status: Accepted interaction-shell decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0027 and ADR 0029

## Context

The current shell fixes both outer layout layers to `100dvh`, hides their overflow, makes the inner `main` element scroll independently and restores its `scrollTop` after an arbitrary delay. That combination causes severe scrolling and navigation instability. Expanding from one navigation item to ten Workspaces also requires an explicit responsive hierarchy rather than shrinking the desktop sidebar or hiding routes behind manual URLs.

## Decision

The responsive shell exposes every Workspace at every supported viewport without reducing mobile to a read-only companion.

- Desktop uses the grouped Workspace sidebar defined by ADR 0027 and permits an explicit collapsed state.
- Tablet uses a stable Lucide icon rail with accessible names and hover or focus Tooltips.
- Mobile uses a safe-area-aware bottom navigation for Overview, Alerts, Agent, Incidents and More. More opens a navigation sheet containing Infrastructure, Monitoring, Logs, Traces, DevOps and Settings as direct native links.
- Mobile retains the same read, configuration, Agent and confirmed-mutation capabilities as desktop. Dense presentation may adapt, but product capability does not disappear.

Each route has exactly one primary vertical scroll owner. Standard Workspaces use browser document scrolling with a sticky shell header and desktop navigation. The full-bleed Operations Atlas owns its remaining viewport instead of creating a second page scrollbar; its resource inspector may scroll independently. Independent scrolling is otherwise limited to bounded controls that require it, including log viewers, horizontal data tables, navigation sheets, drawers and modals.

The current nested `.app-main` scroll container, delayed `scrollTop` writes and route-keyed manual scroll map are removed. Native router and browser history restore document position. Durable Workspace state such as filters, tabs, pagination, time range and selected resource is represented in the URL, so Back and Forward restore operating context rather than depending on DOM timing.

## Consequences

- The shell must use resilient dynamic viewport and safe-area behavior without trapping body scroll or relying on JavaScript height measurement.
- Opening a mobile sheet or modal locks only the background for that overlay and restores focus to its trigger when closed.
- Mobile tables need bounded horizontal scrolling or task-appropriate compact views; they cannot widen the document.
- Skip navigation, route-heading focus and keyboard navigation remain part of the shell while being rebuilt around document scrolling.
- Responsive and scroll acceptance tests cover history traversal, deep links, long pages, drawers, logs, tables and Atlas inspection on desktop and mobile viewports.

## Rejected Alternatives

- Keep the fixed nested scroller and tune its restoration delay.
- Put all ten Workspaces in an overflowing mobile tab bar.
- Hide non-core Workspace routes on mobile.
- Give each dashboard panel its own vertical scrollbar.
