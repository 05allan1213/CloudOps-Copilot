# Gate 1 Browser Results

```text
B1=PASS
B2=NOT RUN
B3=PASS
B4=NOT RUN
B5=PASS
B6=NOT RUN
B7=NOT RUN
B8=NOT RUN
```

Runtime:

- Playwright CLI with `/home/monody/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome`.
- Vite at `http://127.0.0.1:52731`.
- Deterministic fixture at `http://127.0.0.1:18082`.
- Locale `zh-CN`, timezone UTC, reduced motion enabled.

## Retained Captures

| File | Result |
| --- | --- |
| `b1-dark-1440x900.png` | Dark canonical Token mapping, existing route ready state |
| `b1-light-1440x900.png` | Persisted Light mapping after duplicate color-mode fix |
| `b3-light-1280x800.png` | Expanded 220px Sidebar, no page overflow |
| `b3-dark-1024x768.png` | 64px rail degradation, no page overflow or overlap |
| `b5-skip-focus-1440x900.png` | Visible Skip Link and Focus ring |

## B1 Theme and Runtime

| Assertion | Light | Dark |
| --- | --- | --- |
| Root class | `light` only | `dark` only |
| `data-theme` | `light` | `dark` |
| Canvas | `rgb(244, 246, 248)` | `rgb(11, 15, 20)` |
| Primary text Token | `#17212b` | `#f1f5f7` |
| Document width | 1440/1440 | 1440/1440 |
| Main width | 1220/1220 | 1220/1220 |

With `cloudops-theme` absent, emulated system Light selected Light and emulated system Dark selected Dark. With stored values present, reload preserved the stored choice. The final state did not contain simultaneous Light/Dark classes.

Console had 0 errors and 0 warnings. Network API traffic was fixture-backed, read-only GET, and HTTP 200. No mutation was observed.

## B3 Desktop Degradation

| Viewport | Theme | Sidebar | Document | Main | Result |
| --- | --- | ---: | ---: | ---: | --- |
| 1280x800 | Light | 220px | 1280/1280 | 1060/1060 | `PASS` |
| 1024x768 | Dark | 64px | 1024/1024 | 960/960 | `PASS` |

Visual inspection found no incoherent overlap, clipped primary control, or page-level horizontal scrolling. The Incident table retains its bounded internal table behavior.

## B5 Keyboard, Focus, and Motion

- Direct route entry focuses `H1[tabindex=-1]`.
- The Skip Link is visible when focused; Enter updates the URL hash and moves Focus to `main#main-content`.
- Notification overlay Tab Focus remains inside its `aria-modal=true` dialog.
- Escape closes the top overlay and restores Focus to `打开通知收件箱`.
- `prefers-reduced-motion: reduce` is active.
- Main entry animation and Sidebar transition both compute to `1e-05s`.

## Classification

This is `FIXTURE` browser evidence. It does not prove a Provider-backed read chain, persistence, or any write side effect.

```text
REAL_READONLY_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
```
