# Installable PWA and Mobile Layout Fixes: Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web UI can be added to a phone's home screen and opens there as a standalone app, it can be refreshed and updated without a browser reload button, and no screen overflows the viewport sideways on a phone.

**Tech Stack:** React, TypeScript, Tailwind v4, Vite, vitest; the Go `internal/web` handler is unchanged. No new Go or npm dependencies. Icons are rendered once with the Playwright headless Chromium already on the dev box and committed as PNGs.

Work on a branch named `pwa-mobile`, cut from `master`.

---

## Design

### What this delivers

1. **Installable app.** A web app manifest, home screen icons, and the iOS meta tags. "Add to Home Screen" on iOS and "Install app" on Android open Tracelet full screen without browser chrome, with the Tracelet name and icon.
2. **Browser chrome follows the theme.** A `theme-color` meta tag follows the light or dark choice, so the iOS status bar and Safari's tinted toolbar match the header.
3. **Safe areas.** The page extends under the home indicator (`viewport-fit=cover`), and every scrolling list and the detail pane leave room for the tab bar plus the bottom inset, so the last row is never hidden.
4. **The two reported overflows are fixed.**
   - The "No projects yet" empty state grows to fit its widest line instead of the screen: the description and the command block are cut off on the right.
   - In the time range popover, iOS draws the custom range's `datetime-local` inputs wider than the panel and taller than `h-9`, with the value pinned to the top.
5. **No zoom on focus.** iOS Safari zooms the page when a focused field's text is under 16 px. Every field is 16 px on touch screens.
6. **A refresh button on phones that also updates the app.** The header's refresh button, today desktop only, shows on phones too. It re-runs the page's searches. When the server's version has changed since the page loaded, the button carries a dot and a tap reloads the whole page instead, which loads the new frontend. An installed app has no browser reload, so without this a deploy would not reach it until the app was killed.
7. **An audit of every screen at phone width** finds and fixes any other horizontal overflow.

Out of scope:
- **A service worker and offline use.** Chrome and Safari install a site without one, an observability dashboard has nothing useful to show offline, and caching the app shell risks running a stale frontend against a newer server after a deploy. Push notifications for alerts would need one; that comes with alerts.
- **Landscape phone layouts beyond safe-area padding.**
- **Pull to refresh.** iOS does not offer it in installed web apps, and a hand-made one fights the scrolling lists. The button covers the need.

### Root causes

**Empty state.** `EmptyState` is `mx-auto max-w-md` inside the shell's column flexbox. Auto margins switch off the cross-axis stretch, so the box takes its fit-content width, which is at least its min-content width. The `<pre>` lines do not wrap, so min-content is the longest command line, which is wider than a phone. The box therefore takes `max-w-md` (448 px) on a 390 px screen. `overflow-x-auto` on the `<pre>` cannot help while its parent is that wide. Adding `w-full` fixes the box at the container's width, capped by `max-w-md`, and the `<pre>` scrolls inside it.

**Date inputs.** WebKit on iOS gives `datetime-local` a native appearance with an intrinsic minimum width and its own height, and it centres the value. `appearance-none` plus `min-w-0` makes the input honour `w-full` and `h-9`. A rule on `::-webkit-date-and-time-value` aligns the value left. Chromium does not reproduce this, so only a phone can confirm it (Task 8).

### Key decisions

- **`manifest.json`, not `manifest.webmanifest`.** Go's built-in MIME table maps `.json` to `application/json`, which browsers accept for manifests. `.webmanifest` is not in the table, and the Docker image has no `/etc/mime.types`, so it would be sniffed as `text/plain`. Serving it as a plain static file needs no Go change: the frontend handler already serves files outside `/assets/` without authentication and with `no-cache`, and the browser fetches manifests without credentials.
- **Icons.** One source SVG, `web/icons/app-icon.svg`: the Tracelet mark in the light accent on a full-bleed white square. The mark sits inside the central 80 % circle, so the same art serves as a maskable Android icon. iOS fills transparent pixels with black and rounds the corners itself, so the square is opaque and square-cornered. Three PNGs in `web/public/` come from it: `icon-192.png`, `icon-512.png`, and `apple-touch-icon.png` (180 px). The manifest lists `icon-512.png` twice, once as `any` and once as `maskable`.
- **Status bar style `default`, not `black-translucent`.** `black-translucent` draws white status bar text over the page, which is invisible on the white light-theme header, and would also need the header and the detail pane's `top-14` to account for the top inset. With `default`, the page starts below the status bar and iOS colours the bar from `theme-color`.
- **`theme-color` tracks the header colour, `--surface`.** `applyTheme` in `theme.ts` sets the meta tag from the computed `--surface` value. The inline script in `index.html` runs before the stylesheet loads, so it writes the dark value as a literal with a comment that points to `index.css`. The tag's default in the HTML is the light value.
- **`start_url` is `/`.** The router redirects `/` to `/logs`, and the project resolution in `useProject` restores the last project, so the app opens where the user left off.
- **The 16 px field rule is plain CSS in `index.css`, outside any layer.** Unlayered rules beat Tailwind's `@layer utilities`, so one rule covers every current and future field, under `@media (pointer: coarse)`. Desktop keeps its compact 13–14 px fields. The alternative, `text-base md:text-sm` on each input, is easy to forget on the next one. `maximum-scale=1` in the viewport tag is rejected because it also disables pinch zoom on Android.
- **One `pb-tabbar` utility for the space under lists.** Five scrolling regions and the pane repeat `pb-16 md:pb-0` for the tab bar. That now has to add `env(safe-area-inset-bottom)`, so it becomes an `@utility` in `index.css`: `padding-bottom: calc(4rem + env(safe-area-inset-bottom))`. Call sites write `pb-tabbar md:pb-0`.

- **Refresh re-runs searches; a full reload is kept for a new version.** Re-running searches is fast and keeps scroll position and selection, so it stays the default. Reloading only when the version changed covers the one case a query refresh cannot: the frontend itself is stale. The version the page loaded with is the first `version` the health endpoint returned, recorded by the always-mounted shell rather than the button. The frontend build does not know the Go binary's version, and baking one in at Vite build time would disagree with dev builds and dirty trees. The health query already polls every 10 seconds, so the dot appears within 10 seconds of a deploy, with no new request. The health query's options move into a shared `healthQuery`, the way `sessionQuery` is shared, so the dot and the button read one cache entry.
- **The button stays where the desktop has it: on the list pages.** The trace page already has its own refresh beside the trace ID.

### Testing

- `web/src/manifest.test.ts` (vitest, node) reads `web/public/manifest.json`. It checks the required fields, that each icon file exists, and that each PNG's real size, read from its IHDR chunk, matches the `sizes` it declares. It also checks that `index.html` links the manifest and the apple touch icon. A renamed or resized icon fails here instead of silently breaking installation.
- `web/src/version.test.ts` covers `versionWatch`: no version yet is not stale, the first version is remembered, the same version is not stale, a different one is, `undefined` after a version (the health request failed) is not stale, and the first version stays the reference after a change.
- `theme.ts` touches the DOM, and the vitest environment is node, so the meta tag sync is checked in the browser during the audit instead of with a unit test.
- Layout is checked by the audit (Task 7) in Chromium at 390×844, and on a real iPhone (Task 8) for the iOS-only behaviour.

---

## File Structure

| File | Change |
| --- | --- |
| `web/icons/app-icon.svg` | Create. Source art for the PNG icons; not shipped. |
| `web/public/icon-192.png`, `icon-512.png`, `apple-touch-icon.png` | Create. Rendered from the SVG; Vite copies `public/` into the build root. |
| `web/public/manifest.json` | Create. The web app manifest. |
| `web/index.html` | Modify. Viewport `viewport-fit=cover`, manifest and touch icon links, `theme-color`, iOS app meta tags, theme script sets `theme-color`. |
| `web/src/manifest.test.ts` | Create. Manifest and icon checks. |
| `web/src/theme.ts` | Modify. `applyTheme` syncs `theme-color`. |
| `web/src/index.css` | Modify. `pb-tabbar` utility, 16 px fields on touch screens, date value alignment. |
| `web/src/components/States.tsx` | Modify. `EmptyState` gets `w-full`. |
| `web/src/components/TimeRangePicker.tsx` | Modify. Date inputs get `min-w-0 appearance-none`. |
| `web/src/components/Pane.tsx`, `web/src/pages/{Logs,Traces,Trace,Metrics,Metric}Page.tsx` | Modify. `pb-16` becomes `pb-tabbar`. |
| `web/src/version.ts`, `web/src/version.test.ts` | Create. `versionWatch`, which remembers the first server version and reports a later different one. |
| `web/src/session.ts` | Modify. Exports `healthQuery` beside `sessionQuery`. |
| `web/src/components/AppShell.tsx` | Modify. `HealthDot` uses `healthQuery`; `RefreshButton` shows on phones and reloads the page when the version changed. |
| Whatever the audit finds | Modify. |
| `docs/DESIGN.md` | Modify. A short "Installable app" paragraph in "UI and dashboards", and a row in the decisions table. |
| `README.md` | Modify. One sentence on adding the UI to a phone's home screen, if the README describes the UI. |

---

### Task 1: Branch

- [x] **Step 1: Cut the branch**
  Run: `git checkout master && git pull --ff-only && git checkout -b pwa-mobile`

### Task 2: Icons and the manifest

**Files:**
- Create: `web/icons/app-icon.svg`, `web/public/icon-192.png`, `web/public/icon-512.png`, `web/public/apple-touch-icon.png`, `web/public/manifest.json`
- Test: `web/src/manifest.test.ts`

- [x] **Step 1: Draw the source SVG**
  A 512×512 `viewBox`, a full-bleed `#ffffff` rect, then the three bars of `Mark` (`web/src/components/icons.tsx`) in `#2f4fd6` with opacities 1, 0.7 and 0.45. Scale and centre the bars so their bounding box fits inside a centred circle of radius 0.4 × 512 (the maskable safe zone). About 260 px wide is right. No rounded corners on the square.

- [x] **Step 2: Render the PNGs**
  Write a throwaway `.tmp/icon.html` that shows the SVG at 100 % of a page with zero margin. Render it at each size with the headless shell:
  ```bash
  HS=~/.cache/ms-playwright/chromium_headless_shell-1243/chrome-linux/chrome-headless-shell
  for s in 192 512 180; do
    "$HS" --headless --hide-scrollbars --force-device-scale-factor=1 \
      --window-size=$s,$s --screenshot=.tmp/icon-$s.png "file://$PWD/.tmp/icon.html"
  done
  ```
  Then move them to `web/public/icon-192.png`, `web/public/icon-512.png`, and `web/public/apple-touch-icon.png`. If the binary's directory differs, find it with `ls ~/.cache/ms-playwright/`. Open each PNG with the Read tool and check that it is the mark, centred, on white, with no scrollbar or margin.

- [x] **Step 3: Write `web/public/manifest.json`**
  ```json
  {
    "id": "/",
    "name": "Tracelet",
    "short_name": "Tracelet",
    "description": "Self-hosted logs, traces, and metrics",
    "start_url": "/",
    "scope": "/",
    "display": "standalone",
    "background_color": "#e7ebf1",
    "theme_color": "#ffffff",
    "icons": [
      { "src": "/icon-192.png", "sizes": "192x192", "type": "image/png" },
      { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png" },
      { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
    ]
  }
  ```
  `background_color` is the light `--canvas`, shown on the Android splash screen.

- [x] **Step 4: Write the failing test `web/src/manifest.test.ts`**
  Read files relative to `import.meta.dirname` with `node:fs`. Cases:
  - the manifest has `name`, `short_name`, `start_url`, and `display` equal to `standalone`, and has a 192 and a 512 icon with purpose `any` (or none) and one with purpose `maskable`;
  - every icon `src` exists under `web/public/`, and its width and height match `sizes`. A PNG stores width and height as big-endian 32-bit integers at bytes 16 and 20 (`buf.readUInt32BE(16)`, `buf.readUInt32BE(20)`);
  - `web/public/apple-touch-icon.png` is 180×180;
  - `web/index.html` contains `rel="manifest" href="/manifest.json"` and `rel="apple-touch-icon" href="/apple-touch-icon.png"`.

- [x] **Step 5: Run it to see the `index.html` case fail**
  Run: `cd web && npx vitest run src/manifest.test.ts`
  Expected: the manifest and icon cases pass; the `index.html` case fails until Task 3.

- [x] **Step 6: Commit**
  `git add web/icons web/public web/src/manifest.test.ts && git commit -m "Add the web app manifest and home screen icons"`
  The commit leaves one failing test that Task 3 fixes straight away. If you prefer every commit green, commit Tasks 2 and 3 together.

> Deviation (Task 2): the headless shell is at `chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell`, and needs `--no-sandbox` here. Tasks 2 and 3 were committed together (`2ff9074`), as the plan allows, so no commit is red.
> Deviation (Task 2/3 review): `tsc -b` type-checks `manifest.test.ts` under `tsconfig.app.json`, which has no Node types, so `rite check` failed (TS2591). The test moved to `tsconfig.node.json` and is excluded from the app config (`908db1b`). Found by codex; I had run only vitest.

### Task 3: `index.html` and the theme colour

**Files:**
- Modify: `web/index.html`, `web/src/theme.ts`

- [x] **Step 1: Update the head of `web/index.html`**
  - viewport: `width=device-width, initial-scale=1.0, viewport-fit=cover`
  - `<link rel="manifest" href="/manifest.json" />`
  - `<link rel="apple-touch-icon" href="/apple-touch-icon.png" />`
  - `<meta name="theme-color" content="#ffffff" />`
  - `<meta name="mobile-web-app-capable" content="yes" />` and `<meta name="apple-mobile-web-app-capable" content="yes" />`. Older iOS versions only read the `apple-` name.
  - `<meta name="apple-mobile-web-app-status-bar-style" content="default" />`
  - `<meta name="apple-mobile-web-app-title" content="Tracelet" />`
  - In the inline theme script, when the stored theme is dark, also set the `theme-color` tag's `content` to `#111827`, with a comment that it is the dark `--surface` in `src/index.css`. The script runs in `<head>`, so place it after the `theme-color` tag.

- [x] **Step 2: Sync `theme-color` in `applyTheme`**
  After toggling the class, read `getComputedStyle(document.documentElement).getPropertyValue('--surface').trim()` and, if it is not empty, set it as the `content` of `meta[name="theme-color"]`. Do nothing if the tag is missing. Update the file's top comment to say that the theme also sets the browser's theme colour.

- [x] **Step 3: Run the frontend tests**
  Run: `cd web && npm test`
  Expected: all pass, including `manifest.test.ts`.

- [x] **Step 4: Commit**
  `git commit -am "Make the UI installable and tint the browser chrome by theme"`

### Task 4: Fix the two reported overflows

**Files:**
- Modify: `web/src/components/States.tsx`, `web/src/components/TimeRangePicker.tsx`, `web/src/index.css`

- [x] **Step 1: Empty state**
  In `EmptyState`, add `w-full` to the outer `div` (`mx-auto w-full max-w-md px-6 py-16 text-center`). Add a one-line comment saying why: auto margins in the shell's column flexbox would otherwise size the box to its widest unbreakable line.

- [x] **Step 2: Date inputs**
  On both `datetime-local` inputs in `TimeRangePicker.tsx`, add `min-w-0 appearance-none`. In `web/src/index.css`, add, unlayered, with a comment that iOS centres the value:
  ```css
  input::-webkit-date-and-time-value {
    text-align: left;
  }
  ```

- [x] **Step 3: Check at phone width**
  Build and serve as in Task 7, Step 1. At 390×844 with no projects, the empty state's text wraps inside the screen and the command block scrolls inside its box. With the time range popover open, both inputs end inside the panel. Run the overflow probe from Task 7, Step 2 on both; it must return no elements.

- [x] **Step 4: Commit**
  `git commit -am "Keep the empty state and the custom range inside a phone screen"`

> Deviation (Task 4): the preview tools had no host in this environment, so every browser check uses headless Chromium through `playwright-core` from `/tmp/tl-e2e` (outside the repo, no dependency added), with touch emulation at 390×844 (`pointer: coarse` is true). Probe scripts are in `.tmp/`.
> Deviation (Task 4): the probe found a third overflow the plan did not list. The time range panel (`w-80`, `right-0` under a button mid-row) hung 4.6 px off the left edge at 390 px. `Popover` panels now use `fixed` plus `left-3`/`right-3` below `md`, and `absolute` anchored to the button from `md` up (`62bab9d`). This also covers the filter popovers and the account menu.

### Task 5: Safe-area padding and no zoom on focus

**Files:**
- Modify: `web/src/index.css`, `web/src/components/Pane.tsx`, `web/src/pages/LogsPage.tsx`, `web/src/pages/TracesPage.tsx`, `web/src/pages/TracePage.tsx`, `web/src/pages/MetricsPage.tsx`, `web/src/pages/MetricPage.tsx`

- [x] **Step 1: Add the utility**
  In `web/src/index.css`, next to `error-hatch`:
  ```css
  /* Room under a scrolling region for the phone tab bar (h-14) and the home indicator. */
  @utility pb-tabbar {
    padding-bottom: calc(4rem + env(safe-area-inset-bottom));
  }
  ```

- [x] **Step 2: Use it**
  Replace `pb-16` with `pb-tabbar` in the scrolling container of each page listed above and in `Pane`'s `aside`. Keep `md:pb-0`. Then confirm nothing is left:
  Run: `grep -rn "pb-16" web/src`
  Expected: no output.

- [x] **Step 3: 16 px fields on touch screens**
  In `web/src/index.css`, unlayered, with a comment that iOS Safari zooms into any focused field under 16 px, and that unlayered rules win over Tailwind's utilities:
  ```css
  @media (pointer: coarse) {
    input,
    select,
    textarea {
      font-size: max(16px, 1em);
    }
  }
  ```
  Check in Chromium with touch emulation (Task 7) that the search box, the filter inputs, and the login field still fit their boxes at 16 px. A truncated placeholder in the search box is acceptable.

- [x] **Step 4: Run the frontend checks**
  Run: `cd web && npm run lint && npm run typecheck && npm test`
  Expected: all pass.

- [x] **Step 5: Commit**
  `git commit -am "Leave room for the home indicator and stop iOS zooming into fields"`

> Note (Task 5): at 390 px with 16 px fields the search box placeholder truncates to "Search m", which the plan accepts. Checked in Chromium with touch emulation: fields compute to 16 px, `pb-tabbar` to 64 px (the inset is 0 there), no overflow.

### Task 6: A refresh button on phones that reloads after a deploy

**Files:**
- Create: `web/src/version.ts`, `web/src/version.test.ts`
- Modify: `web/src/session.ts`, `web/src/components/AppShell.tsx`

- [x] **Step 1: Write the failing test `web/src/version.test.ts`**
  The cases listed under Testing in the design. The module's interface:
  ```ts
  /** Remembers the first server version it sees; true once a later one differs. */
  export function versionWatch(): (version: string | undefined) => boolean
  ```

- [x] **Step 2: Run it to see it fail**
  Run: `cd web && npx vitest run src/version.test.ts`
  Expected: FAIL, the module does not exist.

- [x] **Step 3: Implement `web/src/version.ts`**
  A closure over the first defined version. A top comment says why: an installed app has no browser reload, so a changed server version is the signal that the loaded frontend is stale.

- [x] **Step 4: Run it to see it pass**
  Run: `cd web && npx vitest run src/version.test.ts`
  Expected: PASS

- [x] **Step 5: Share the health query**
  In `web/src/session.ts`, add `healthQuery = queryOptions({ queryKey: ['health'], queryFn: getHealth, refetchInterval: 10_000, retry: false })` and make `HealthDot` use it. The refresh button's predicate already skips the `health` key; keep that.

- [x] **Step 6: Update `RefreshButton` in `AppShell.tsx`**
  - Create one watch at module scope in `AppShell.tsx` with a comment that it lives for the page load, which is the point: `const serverVersion = versionWatch()`.
  - Compute `stale` in `AppShell` itself, not in `RefreshButton`: `AppShell` is mounted on every signed-in page, while the button is only on list pages. Otherwise a page opened on a trace would take the first version it saw on a list page, after a deploy, as its baseline, and never show the dot. `AppShell` reads `useQuery(healthQuery)`, computes `stale = serverVersion(health.data?.version)` on every render, and passes `stale` through `Header` to `RefreshButton`.
  - Show the button at every width: `grid` instead of `hidden md:grid`. The header still shows it only on list pages.
  - When `stale`, draw a small accent dot at the icon's top right, set the title and `aria-label` to "A new version is available: reload", and on tap call `window.location.reload()`. Otherwise keep today's title and the query invalidation.

- [x] **Step 7: Check it at phone width**
  In the audit's server (Task 7, Step 1), at 320×640 on the logs page, the header shows the logo, the project, refresh, health, theme, and account without overflowing, and the project name truncates. To see the stale state, restart the server built with a different version (`go run -ldflags "-X main.version=test2" ./cmd/tracelet serve ...`); within 10 seconds the dot appears, and a tap reloads the page and clears it. Repeat starting on a trace page: open a trace directly, restart the server with another version, wait 10 seconds, then go to Logs; the dot must be there.

- [x] **Step 8: Run the frontend checks**
  Run: `cd web && npm run lint && npm run typecheck && npm test`
  Expected: all pass.

- [x] **Step 9: Commit**
  `git add web/src/version.ts web/src/version.test.ts && git commit -am "Refresh on phones, and reload the page after a deploy"`

> Note (Task 6): verified in Chromium with a server restarted under another `-X main.version`. At 320×640 the header fits and the project name truncates; the dot appeared within 10 s of the restart, a tap reloaded the page and cleared it, and starting on a trace page then going to Logs after a restart also showed the dot. At 320 px the search box shrinks to a sliver; the audit looks at it.

### Task 7: Audit every screen at phone width

The only way to find the remaining overflows is to look. The audit needs a server with data in every view.

- [x] **Step 1: Run a server with sample data**
  - Build: `rite web-build`
  - Start: `go run ./cmd/tracelet serve --data-dir .tmp/pwa-data` in the background. It prints the admin token on first start.
  - Create a project and an ingest token with `go run ./cmd/tracelet ...`, as in the README.
  - Write a throwaway seeder, `.tmp/seed/main.go`, that uses `go.opentelemetry.io/proto/otlp` (already in `go.mod`) and `google.golang.org/protobuf/proto` to POST protobuf to `/v1/logs`, `/v1/traces`, and `/v1/metrics` with the ingest token. Make the data stress the layout: a log body of 400 characters with no spaces, an attribute with a long URL value, a service name of 40 characters, a trace with 30 spans nested 6 deep with long span names, and a gauge and a histogram with several series. The request builders in the `internal/httpapi` tests show the shapes. Run it from the repo root with `go run .tmp/seed/main.go`, so it resolves imports against the repo's `go.mod`. If Go refuses a file under a dot directory, put it in a temporary directory outside the repo with its own `go.mod` and `go get` the two modules.

- [x] **Step 2: Probe each screen**
  Open the app with the preview tools (`preview_open`, then `preview_resize` to 390×844), sign in with the admin token, and visit each screen below. On each, run this with `preview_evaluate` and take a `preview_snapshot` or screenshot:
  ```js
  (() => {
    const w = document.documentElement.clientWidth
    const wide = [...document.querySelectorAll('body *')]
      .filter((e) => { const r = e.getBoundingClientRect(); return r.right > w + 1 || r.left < -1 })
      .filter((e) => !e.closest('[class*="overflow-x-auto"], [class*="overflow-auto"]'))
      .map((e) => `${e.tagName.toLowerCase()}.${String(e.className).slice(0, 80)}`)
    return { scrollWidth: document.documentElement.scrollWidth, width: w, wide: wide.slice(0, 20) }
  })()
  ```
  `scrollWidth` must equal `width`, and `wide` must be empty. Run the whole list at 390×844, then repeat screens 3 to 6 at 320×640, the narrowest phone still in use. Turn on touch emulation if the preview tools offer it, so the `pointer: coarse` field rule applies; check that `matchMedia('(pointer: coarse)').matches` is true before judging field sizes. Screens:
  1. Login page.
  2. No projects (a fresh data dir, signed in with the admin token).
  3. Logs list; with Filters open; with each filter popover open; with the time range popover open; with Live on.
  4. A selected log's detail pane, including the long body and long attribute.
  5. Traces list, a trace with its waterfall, and a selected span's detail.
  6. Metrics catalog, and a metric chart with its legend.
  7. The account menu, the Notice for a rejected project (`?project=nope`), and the header with the refresh button at 320 px.
  8. Dark theme on any two of the above, and check that `document.querySelector('meta[name=theme-color]').content` is `#111827`; switch back and check `#ffffff`.

- [x] **Step 3: Fix what the probe finds**
  Fix each overflow at its cause, in the style of Task 4: `min-w-0` on flex children that hold long text, `break-all` or `truncate` for unbroken identifiers, `w-full` instead of fit-content sizing, `overflow-x-auto` only where scrolling is the right answer (code, the waterfall). Never hide overflow on `body` or the shell to mask a cause. Re-run the probe on the screen after each fix.

- [x] **Step 4: Commit the fixes**
  One commit per screen or per cause, each saying what overflowed, for example `git commit -am "Wrap long span names in the span detail on phones"`. If the probe finds nothing, there is nothing to commit.

- [x] **Step 5: Stop the server and remove the throwaway data**
  Stop the background server. `rm -rf .tmp/pwa-data .tmp/seed .tmp/icon*`

> Note (Task 7): the audit ran 22 screens at 390×844 and again at 320×640 (login, no projects, logs with every filter and popover, live, log detail, traces, waterfall, span detail, metrics, both chart kinds, account menu, rejected project, dark theme). After the Task 4 popover fix every screen passes the overflow probe (`scrollWidth` equals the width, no elements outside it), so no further commits. `theme-color` reads `#111827` in dark and `#fff` in light: the CSS minifier shortens `#ffffff`, the same colour. Throwaway servers, data, seeder and probes are removed.

### Task 8: Docs and the final check

**Files:**
- Modify: `docs/DESIGN.md`, `README.md`

- [x] **Step 1: DESIGN.md**
  In "UI and dashboards", after the metrics view paragraph, add an "Installable app (built)" paragraph: a manifest and icons make the UI installable on phones and desktops; there is no service worker, because there is nothing useful offline and a cached shell could outlive a deploy; `theme-color` follows the theme. The header's refresh button re-runs searches, and reloads the page when the health endpoint reports a different server version than the page loaded with, because an installed app has no reload of its own. Add a row to the decisions table: "Installable UI | Manifest and icons, no service worker | Installs on iOS and Android; no stale app shell after a deploy; push for alerts would add one".

- [x] **Step 2: README.md**
  If the README describes signing in to the UI, add one sentence: on a phone, use "Add to Home Screen" (iOS) or "Install app" (Android) to open Tracelet as an app.

- [x] **Step 3: Run the full check**
  Run: `rite check`
  Expected: format check, lint, and tests pass for Go and the frontend, and the CLI build check passes. If formatting fails, run `rite fmt` and re-run.

- [x] **Step 4: Commit**
  `git commit -am "Document the installable UI"`

- [ ] **Step 5: Hand the phone checks to the user**
  These need a real iPhone after deploy; report them as open, not done:
  - The custom range inputs fit the popover and show their value left-aligned, at the same height as other fields.
  - Focusing the search box does not zoom the page.
  - "Add to Home Screen" shows the Tracelet icon and name; the app opens without Safari's toolbar.
  - In the installed app, the status bar matches the header in both themes, and the tab bar clears the home indicator.
  - After the next deploy, the installed app's refresh button shows its dot, and a tap loads the new version.


---

## Completion

**Status: implemented on branch `pwa-mobile`; the real-iPhone checks (Task 8, Step 5) are open.**

### Summary

The UI is installable: manifest, three icons, iOS meta tags, `viewport-fit=cover`, and a `theme-color` that follows the theme. The empty state and the custom range inputs no longer overflow, popovers stay on screen on phones, lists clear the tab bar plus the home indicator (`pb-tabbar`), and fields are 16 px on touch screens. The refresh button shows on phones, carries a dot when the server version changed since the page loaded, and then reloads the page. The audit of every screen at 390 and 320 px found nothing else. `rite check` passes, and the built server serves the manifest and icons unauthenticated with the right MIME types.

### Deviations

- Headless Chromium path is `chrome-headless-shell-linux64/` and needs `--no-sandbox`. Tasks 2 and 3 were committed together.
- `manifest.test.ts` moved to `tsconfig.node.json` and is excluded from the app config; `tsc -b` failed on Node imports (found by codex, not by vitest).
- The preview tools had no host here, so all browser checks used headless Chromium through `playwright-core` from `/tmp/tl-e2e` (outside the repo, no dependency added) with touch emulation.
- A third overflow, not in the plan: popover panels hung off the left edge. `Popover` is now `fixed` at the screen edge below `md` (`62bab9d`).
- Two backlog entries added as their own commits: a deploy during sign-in is not flagged (codex P2 on Task 6, advisory because the plan decided where the baseline lives), and the 320 px search box sliver.

### Open: needs a real iPhone after deploy

- The custom range inputs fit the popover, show their value left-aligned, and match other fields in height.
- Focusing the search box does not zoom the page.
- "Add to Home Screen" shows the Tracelet icon and name; the app opens without Safari's toolbar.
- In the installed app, the status bar matches the header in both themes, and the tab bar clears the home indicator.
- After the next deploy, the installed app's refresh button shows its dot, and a tap loads the new version.

**What the plan could have specified better:** it described the audit as a check of the two known overflows plus a sweep, but the first probe of the time range popover needed the panel's own position checked, not only the inputs; it also assumed the preview tools and the `chrome-linux` path would exist, and it did not say the seeded data ages out of the default 1 h range during a long audit.
