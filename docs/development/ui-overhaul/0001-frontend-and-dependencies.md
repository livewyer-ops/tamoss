# 0001: Frontend and Dependency Budget

## Status

- Accepted for the TAMOSS 8.2 UI.
- The blank-slate shell, bounded TAMS catalog pages, CSS Modules, Biome, lazy
  routes, and build-budget check are implemented on the 8.2 development branch.
- The current production build is 86.80 KiB initial JavaScript gzip with six
  direct runtime packages excluding Omakase, and 311 lockfile package entries,
  down from 338. `npm audit --omit=dev` reports three moderate advisories, all
  reaching production only through the lazy Omakase preview chunk; see
  [0004](0004-omakase-preview.md).
- The measurable checks under [8.2 Release Gates](#82-release-gates) remain
  open.

## Context

The existing React UI has useful TAMS API knowledge and tests, but its browser
ingest path, custom playback path, styling stack, and development tooling make
the dependency graph expensive to maintain. Browser FFmpeg is also responsible
for most of the shipped asset size.

Changing framework would not remove the main sources of complexity. Omakase
has a framework-neutral API, React is already understood in this repository,
and the UI needs mature routing, server-state handling, testing, and accessible
interaction patterns.

A clean Vite 8.2.1 spike on 9 August 2026 measured these production graphs and
empty-shell JavaScript sizes before application code:

| Shell | Lockfile packages | JavaScript gzip |
| --- | ---: | ---: |
| React | 69 | 60.62 KiB |
| Preact | 115 | 7.29 KiB |
| Svelte | 74 | 12.23 KiB |
| Browser APIs only | 40 | 2.02 KiB |

Removing the scaffold's Oxlint reduced React to 49 packages; using pinned Biome
produced 58. By contrast, adding Omakase 1.1.1 added 112 production packages to
the browser-only baseline. Framework choice is therefore a secondary control;
player isolation and tooling discipline remove more maintenance risk.

## Accepted Decisions

The UI remains a React 19, TypeScript, and Vite application. This is a clean
component and visual rewrite, not a requirement to preserve existing page
components.

The frontend uses these boundaries:

- React Router owns route state and lazy route loading.
- TanStack Query owns remote state. React state and context own local UI state;
  no general global state library is added initially.
- OpenAPI-generated operation types plus one reviewed typed adapter per API are
  the ordinary access path to the TAMS and Console contracts. Page components
  do not call `fetch` directly.
- CSS Modules and shared CSS custom properties replace Tailwind. A small local
  component layer owns layout, form, table, dialog, status, and feedback
  patterns.
- Semantic HTML and native controls are preferred. A headless component
  dependency is accepted only when a native control cannot meet the keyboard,
  focus, and screen-reader contract reliably.
- Lucide supplies product icons. Do not add a second icon set or hand-copy SVG
  paths into components.
- Biome replaces ESLint, its plugins, and Prettier. TypeScript remains the
  type-checker; Vitest and Testing Library remain the component test tools.
- Omakase is isolated behind the adapter in
  [record 0004](0004-omakase-preview.md). Its code and styles are not part of
  the application shell chunk.

The new UI does not ship browser FFmpeg, a second direct HLS implementation, a
general design-system package, or a second remote-state library. Media ingest
runs in Tamsin Jobs as defined in
[record 0005](0005-tamsin-ingest-runs.md).

## Dependency Policy

A new runtime dependency needs a short review in the change that introduces
it:

1. Identify the user-facing or maintenance problem it solves.
2. Record why browser or platform APIs and existing dependencies are
   insufficient.
3. Check maintenance activity, licence, published package contents, peer
   ranges, and reachable security findings.
4. State whether it is shell-critical or lazy-loadable.
5. Add focused tests at the adapter boundary so the dependency can be replaced.

The application shell has at most seven direct runtime packages, counting
React and React DOM but excluding the separately budgeted `@byomakase/*`
family. Do not add duplicate packages for routing, queries, state, CSS-in-JS,
icons, dates, or generic utilities.

Omakase packages use exact compatible versions and are upgraded as one unit.
Routine development-only minor and patch updates may be grouped weekly in
Dependabot. Runtime and major updates remain individually reviewable.

## Build Budgets

CI records both compressed assets and the production dependency graph. The
budgets are:

| Measure | Budget |
| --- | --- |
| Initial application JavaScript | At most 200 KiB gzip |
| Omakase in initial application chunk | 0 bytes |
| Direct shell runtime packages | At most 7 |
| Known reachable runtime findings | No high or critical findings |
| Duplicate framework/state/player packages | None |

The player route has a separate reported budget after the compatibility spike;
it is not hidden inside the 200 KiB shell target. Budget changes require a
recorded reason and before/after measurements.

## Migration Rules

Preserve generated API types, verified domain formatting utilities, route
deep-links, and behavioural tests where they still describe the new product.
Do not carry forward browser ingest, custom HLS synchronisation, Tailwind class
structures, page-local SVG icons, or unbounded list accumulation.

The blank-slate UI remains on its development branch until its critical
journeys pass. The release switches the image as one tested unit; the legacy
implementation is not maintained as a permanent second UI.

## 8.2 Release Gates

- Enforce the dependency and compressed build reports in CI.
- Demonstrate that loading Overview or Library does not fetch Omakase code.
- Pass component tests, keyboard navigation checks, and deployed Playwright
  journeys for login, catalog navigation, preview, ingest, and operations.
- Run the new image on Kind through `192.168.122.103`, then through Authentik
  and real TLS on `cnm-tamoss-1`.
- Remove browser FFmpeg, the custom player, Tailwind, ESLint, and Prettier from
  the final production branch and lockfile.

## References

- [Biome documentation](https://biomejs.dev/)
- [Dependabot grouping options](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference#groups)
