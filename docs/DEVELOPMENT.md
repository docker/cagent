# Documentation Site — Development Guide

The documentation site is a **static GitHub Pages site** built with vanilla HTML, CSS, and JavaScript — **no build tools, no frameworks, no npm**. Serve it with any static file server.

GitHub Pages is configured to serve from the `docs/` directory on the `main` branch.

---

## Architecture Overview

### Routing

The site uses **hash-based client-side routing**. Every page is a standalone HTML fragment loaded via `fetch()` into the `#page-content` container. Deep linking to sections is supported via the `page:section` format.

- URL format: `https://site/#concepts/agents` → fetches `pages/concepts/agents.html`
- Deep links: `https://site/#features/rag:configuration-reference` → scrolls to heading
- The home page is `#home` → `pages/home.html`
- No URL without a hash means home (handled in `js/app.js` init)

### Key Files

| File | Purpose |
|------|---------|
| `index.html` | Outer shell: header, sidebar placeholder, content area, search overlay, Prism.js, skip-link, OG meta tags. **All pages render inside this.** |
| `js/app.js` | Everything dynamic: the `NAV` array (source of truth for sidebar + search + prev/next), hash router, full-text search with background indexing, TOC generation, heading IDs, theme toggle, copy-to-clipboard, OG meta updates. |
| `css/style.css` | Full stylesheet: light + dark themes via `[data-theme="dark"]`, responsive breakpoints at 1280px/1024px/640px, TOC styles, print stylesheet, skip-link, all component styles. |
| `pages/**/*.html` | Content-only HTML fragments. No `<html>`, `<head>`, or `<body>` — just the markup that goes inside `.content`. |
| `recordings/` | VHS tape files and GIFs used for demo recordings. |

### How to Add a New Page

1. Create `pages/section/slug.html` with content (no boilerplate, just content HTML).
2. Add an entry to the `NAV` array in `js/app.js`:
   ```js
   { title: 'Page Title', page: 'section/slug' }
   ```
3. That's it. The sidebar, search, prev/next nav, TOC, and heading IDs are all auto-generated.

### Styling Conventions Used in Pages

Pages use these CSS patterns (all defined in `style.css`):

| Element | Usage |
|---------|-------|
| `<p class="subtitle">` | Gray subtitle below `<h1>`, used on every page |
| `<div class="callout callout-info/tip/warning">` | Admonition boxes. Contain a `<div class="callout-title">` + `<p>` |
| `<div class="cards">` + `<a class="card">` | Grid of clickable cards with `card-icon`, `h3`, `p` |
| `<div class="features-grid">` + `<div class="feature">` | Feature showcase grid (home + intro pages) |
| `<pre><code class="language-yaml/bash/json/go">` | Code blocks with Prism.js syntax highlighting |
| `<kbd>` | Keyboard shortcut badges |
| `<div class="hero">` | Hero banner (only on home page) |
| `<div class="demo-container"><img>` | Demo GIF container (home + TUI pages) |

Internal links use this pattern to work with the hash router:
```html
<a href="#concepts/agents" onclick="event.preventDefault(); navigate('concepts/agents')">Link text</a>
```

### Theme System

- Light is default. Dark activates via `[data-theme="dark"]` on `<html>`.
- Persisted in `localStorage` key `cagent-docs-theme`.
- Auto-detects `prefers-color-scheme: dark` on first visit.
- Toggle button in header calls `toggleTheme()`.

### Search

Search is **full-text** — pages are background-indexed after initial load. The index includes page titles, section headings, and full page content. Results are ranked by match type (title > heading > content). Opens with ⌘K / Ctrl+K or by clicking the search input.

### Table of Contents

Long pages automatically get a sticky right-side TOC generated from `<h2>` and `<h3>` headings. The TOC:
- Appears on screens ≥1280px
- Uses scroll-spy to highlight the current section
- Supports click-to-scroll with deep link URLs
- Auto-hides for short pages (<3 headings)

---

## How to Preview Locally

```bash
cd docs
python3 -m http.server 8000
# Open http://localhost:8000
```

Or use any static file server (`npx serve .`, `caddy file-server`, etc.). No build step needed.

## How to Deploy to GitHub Pages

Configure GitHub Pages to serve from the `docs/` directory on the `main` branch:

1. In repo Settings → Pages, set source to **Deploy from a branch**.
2. Select the `main` branch and `/docs` folder.

The `404.html` redirect handles path-based URLs (e.g., `/concepts/agents` → `/#concepts/agents`).

## Re-recording the Demo GIF

The demo GIF is recorded using [VHS](https://github.com/charmbracelet/vhs). Tape files live in `recordings/`:

```bash
# Re-record the main demo
vhs ./docs/recordings/demo.tape

# The output goes to docs/demo.gif
```
