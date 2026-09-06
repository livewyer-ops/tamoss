# Frontend Style Tokens

Styling is plain CSS: global tokens plus per-component CSS Modules. There is no
utility-class framework.

## Tokens

`src/index.css` declares the shared palette on `:root`. Use these for the roles
they name rather than repeating the hex:

- Text — `--ink`, `--muted`, `--subtle`
- Borders — `--line`, `--line-strong`
- Surfaces — `--surface`, `--surface-soft`
- Navigation chrome — `--nav`, `--nav-hover`
- Action and focus — `--accent`, `--focus`
- Status, each with a soft background pair — `--danger`, `--warning`,
  `--success`, `--info`

`index.css` also carries the document-level rules and the only two global
classes: `.srOnly` for visually hidden labels and `.mono` for monospace runs.

## Component styling

Components import a co-located `*.module.css`. The shared primitives live in
`components/Surface.tsx` — `Page`, `PageHeader`, `Panel`, `Button`,
`StatusBadge` and `QueryMessage`, backed by `Surface.module.css`. That module is
also exported as `surfaceStyles` so a page can reach a class directly, which is
how links are given button styling.

Reuse a `Surface` primitive before adding a new module. New modules are for
styling a component owns outright: `Layout.module.css`,
`MediaPreview.module.css` and `IngestRunDetailPage.module.css` are the existing
examples.

Literal colours are acceptable inside a module for shades that belong to one
component and have no shared role — the dark player chrome in
`MediaPreview.module.css`, table zebra striping, badge tints, hover borders.
Anything matching a token role above should use the token, so a palette change
lands in one place.
