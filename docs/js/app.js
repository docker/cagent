/* ===================================================
   cagent docs – client-side router & utilities
   =================================================== */

// ---------- Navigation structure ----------
const NAV = [
  {
    heading: 'Getting Started',
    items: [
      { title: 'Introduction',  page: 'getting-started/introduction' },
      { title: 'Installation',  page: 'getting-started/installation' },
      { title: 'Quick Start',   page: 'getting-started/quickstart' },
    ],
  },
  {
    heading: 'Core Concepts',
    items: [
      { title: 'Agents',        page: 'concepts/agents' },
      { title: 'Models',        page: 'concepts/models' },
      { title: 'Tools',         page: 'concepts/tools' },
      { title: 'Multi-Agent',   page: 'concepts/multi-agent' },
      { title: 'Agent Distribution', page: 'concepts/distribution' },
    ],
  },
  {
    heading: 'Configuration',
    items: [
      { title: 'Overview',      page: 'configuration/overview' },
      { title: 'Agent Config',  page: 'configuration/agents' },
      { title: 'Model Config',  page: 'configuration/models' },
      { title: 'Tool Config',   page: 'configuration/tools' },
      { title: 'Hooks',         page: 'configuration/hooks' },
      { title: 'Permissions',   page: 'configuration/permissions' },
      { title: 'Sandbox Mode',  page: 'configuration/sandbox' },
      { title: 'Structured Output', page: 'configuration/structured-output' },
      { title: 'Model Routing', page: 'configuration/routing' },
    ],
  },
  {
    heading: 'Built-in Tools',
    items: [
      { title: 'LSP Tool',       page: 'tools/lsp' },
      { title: 'User Prompt Tool', page: 'tools/user-prompt' },
      { title: 'API Tool',       page: 'tools/api' },
    ],
  },
  {
    heading: 'Features',
    items: [
      { title: 'Terminal UI',       page: 'features/tui' },
      { title: 'CLI Reference',     page: 'features/cli' },
      { title: 'MCP Mode',          page: 'features/mcp-mode' },
      { title: 'A2A Protocol',      page: 'features/a2a' },
      { title: 'ACP',               page: 'features/acp' },
      { title: 'API Server',         page: 'features/api-server' },
      { title: 'Evaluation',         page: 'features/evaluation' },
      { title: 'RAG',               page: 'features/rag' },
      { title: 'Skills',            page: 'features/skills' },
      { title: 'Remote MCP Servers', page: 'features/remote-mcp' },
    ],
  },
  {
    heading: 'Model Providers',
    items: [
      { title: 'Overview',     page: 'providers/overview' },
      { title: 'OpenAI',       page: 'providers/openai' },
      { title: 'Anthropic',    page: 'providers/anthropic' },
      { title: 'Google Gemini', page: 'providers/google' },
      { title: 'AWS Bedrock',  page: 'providers/bedrock' },
      { title: 'Docker Model Runner', page: 'providers/dmr' },
      { title: 'Mistral',      page: 'providers/mistral' },
      { title: 'xAI (Grok)',   page: 'providers/xai' },
      { title: 'Nebius',       page: 'providers/nebius' },
      { title: 'Local Models', page: 'providers/local' },
      { title: 'Custom Providers',    page: 'providers/custom' },
    ],
  },
  {
    heading: 'Guides',
    items: [
      { title: 'Tips & Best Practices', page: 'guides/tips' },
      { title: 'Go SDK',       page: 'guides/go-sdk' },
    ],
  },
  {
    heading: 'Community',
    items: [
      { title: 'Contributing',     page: 'community/contributing' },
      { title: 'Troubleshooting',  page: 'community/troubleshooting' },
      { title: 'Telemetry',        page: 'community/telemetry' },
    ],
  },
];

// Flat list for search & prev/next, with section pre-computed
const ALL_PAGES = NAV.flatMap(s => s.items.map(item => ({ ...item, section: s.heading })));

// ---------- DOM references ----------
const $content  = document.getElementById('page-content');
const $sidebar  = document.getElementById('sidebar');
const $searchInput = document.getElementById('search-input');
const $searchOverlay = document.getElementById('search-overlay');
const $searchModal   = document.getElementById('search-modal-input');
const $searchResults = document.getElementById('search-results');

// ---------- Build sidebar ----------
function buildSidebar() {
  $sidebar.innerHTML = NAV.map(section => `
    <div class="sidebar-section">
      <div class="sidebar-heading">${section.heading}</div>
      ${section.items.map(item =>
        `<a class="sidebar-link" data-page="${item.page}" href="#${item.page}">${item.title}</a>`
      ).join('')}
    </div>
  `).join('');

  $sidebar.addEventListener('click', (e) => {
    const link = e.target.closest('.sidebar-link');
    if (!link) return;
    e.preventDefault();
    navigate(link.dataset.page);
    $sidebar.classList.remove('open');
  });
}

// ---------- Heading ID generation ----------
function slugify(text) {
  return text.toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

function addHeadingIds(container) {
  const used = {};
  container.querySelectorAll('h2, h3').forEach(el => {
    if (el.id) return;
    let slug = slugify(el.textContent);
    if (used[slug]) {
      slug += '-' + (++used[slug]);
    } else {
      used[slug] = 1;
    }
    el.id = slug;
  });
}

// ---------- Table of Contents ----------
function buildTOC() {
  const existing = document.querySelector('.toc-aside');
  if (existing) existing.remove();

  const headings = $content.querySelectorAll('h2[id], h3[id]');
  if (headings.length < 3) return;

  const aside = document.createElement('aside');
  aside.className = 'toc-aside';
  aside.setAttribute('aria-label', 'Table of contents');
  aside.innerHTML = `
    <div class="toc-inner">
      <div class="toc-title">On this page</div>
      <nav class="toc-nav">
        ${Array.from(headings).map(h => {
          const level = h.tagName === 'H3' ? 'toc-h3' : '';
          return `<a class="toc-link ${level}" href="#${currentPage}:${h.id}" data-id="${h.id}">${h.textContent}</a>`;
        }).join('')}
      </nav>
    </div>`;

  document.querySelector('.main').appendChild(aside);

  aside.addEventListener('click', (e) => {
    const link = e.target.closest('.toc-link');
    if (!link) return;
    e.preventDefault();
    const target = document.getElementById(link.dataset.id);
    if (target) {
      target.scrollIntoView({ behavior: 'smooth', block: 'start' });
      history.replaceState(null, '', `#${currentPage}:${link.dataset.id}`);
    }
  });

  setupScrollSpy(headings, aside);
}

function setupScrollSpy(headings, aside) {
  if (window._tocObserver) window._tocObserver.disconnect();

  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        aside.querySelectorAll('.toc-link').forEach(l => l.classList.remove('active'));
        aside.querySelector(`.toc-link[data-id="${entry.target.id}"]`)?.classList.add('active');
      }
    }
  }, { rootMargin: '-80px 0px -70% 0px', threshold: 0 });

  headings.forEach(h => observer.observe(h));
  window._tocObserver = observer;
}

// ---------- Router ----------
let currentPage = '';
const pageCache = {};

async function navigate(page) {
  let section = null;
  if (!page || page === '/') page = 'home';
  if (page.includes(':')) [page, section] = page.split(':');

  const hash = section ? `${page}:${section}` : page;
  if (location.hash !== '#' + hash) {
    history.pushState(null, '', '#' + hash);
  }

  // Highlight sidebar
  $sidebar.querySelectorAll('.sidebar-link').forEach(l => {
    l.classList.toggle('active', l.dataset.page === page);
  });

  try {
    if (!pageCache[page]) {
      const resp = await fetch(`pages/${page}.html`);
      if (!resp.ok) throw new Error('Page not found');
      pageCache[page] = await resp.text();
    }

    $content.innerHTML = pageCache[page] + buildPageNav(page);
    currentPage = page;

    addHeadingIds($content);
    buildTOC();

    if (section) {
      const target = document.getElementById(section);
      if (target) {
        requestAnimationFrame(() => target.scrollIntoView({ behavior: 'instant', block: 'start' }));
      } else {
        window.scrollTo(0, 0);
      }
    } else {
      window.scrollTo(0, 0);
    }

    // Update page title
    const pageEntry = ALL_PAGES.find(p => p.page === page);
    document.title = pageEntry
      ? `${pageEntry.title} – cagent docs`
      : (page === 'home' ? 'cagent – Documentation' : 'cagent docs');

    updateMetaTags(pageEntry);
    addCopyButtons();
    if (window.Prism) Prism.highlightAllUnder($content);
    indexPageContent(page, pageCache[page]);

  } catch {
    $content.innerHTML = `
      <h1>Page not found</h1>
      <p class="subtitle">The page <code>${page}</code> doesn't exist.</p>
      <a href="#home" class="btn btn-primary" style="background:var(--accent);color:white;">Go Home</a>
    `;
  }
}

// ---------- OpenGraph meta tags ----------
function updateMetaTags(pageEntry) {
  const title = pageEntry ? `${pageEntry.title} – cagent docs` : 'cagent – Documentation';
  const desc = pageEntry
    ? `${pageEntry.title} — ${pageEntry.section} — cagent documentation`
    : 'Build, run, and share AI agents with cagent – a powerful multi-agent system by Docker.';

  for (const [prop, val] of [['og:title', title], ['og:description', desc], ['og:type', 'article'], ['og:url', location.href]]) {
    let el = document.querySelector(`meta[property="${prop}"]`);
    if (!el) {
      el = document.createElement('meta');
      el.setAttribute('property', prop);
      document.head.appendChild(el);
    }
    el.setAttribute('content', val);
  }
}

// ---------- Prev / Next navigation ----------
function buildPageNav(page) {
  const idx = ALL_PAGES.findIndex(p => p.page === page);
  if (idx === -1) return '';

  const prev = idx > 0 ? ALL_PAGES[idx - 1] : null;
  const next = idx < ALL_PAGES.length - 1 ? ALL_PAGES[idx + 1] : null;

  return `<div class="page-nav">
    ${prev
      ? `<a href="#${prev.page}" onclick="event.preventDefault(); navigate('${prev.page}')">
          <span class="nav-label">← Previous</span>
          <span class="nav-title">${prev.title}</span>
        </a>`
      : '<span></span>'}
    ${next
      ? `<a class="next" href="#${next.page}" onclick="event.preventDefault(); navigate('${next.page}')">
          <span class="nav-label">Next →</span>
          <span class="nav-title">${next.title}</span>
        </a>`
      : ''}
  </div>`;
}

// ---------- Full-text Search ----------
const contentIndex = [
  { title: 'Home', page: 'home', section: '', searchText: 'home overview cagent documentation', headings: [], snippet: '' },
  ...ALL_PAGES.map(p => ({
    ...p,
    searchText: `${p.title} ${p.section}`.toLowerCase(),
    headings: [],
    snippet: '',
  })),
];

function indexPageContent(page, html) {
  const entry = contentIndex.find(e => e.page === page);
  if (!entry || entry._indexed) return;

  const tmp = document.createElement('div');
  tmp.innerHTML = html;

  entry.headings = Array.from(tmp.querySelectorAll('h2, h3'), h => h.textContent.trim());
  const firstP = tmp.querySelector('p');
  if (firstP) entry.snippet = firstP.textContent.trim().slice(0, 200);

  entry.searchText = `${entry.title} ${entry.section} ${entry.headings.join(' ')} ${tmp.textContent.replace(/\s+/g, ' ')}`.toLowerCase();
  entry._indexed = true;
}

async function prefetchPagesForSearch() {
  await new Promise(r => setTimeout(r, 500));
  const pages = ['home', ...ALL_PAGES.map(p => p.page)];
  for (const page of pages) {
    try {
      if (!pageCache[page]) {
        const resp = await fetch(`pages/${page}.html`);
        if (resp.ok) pageCache[page] = await resp.text();
      }
      if (pageCache[page]) indexPageContent(page, pageCache[page]);
    } catch { /* ignore fetch errors */ }
    await new Promise(r => setTimeout(r, 50));
  }
}

// ---------- Search UI ----------
function openSearch() {
  $searchOverlay.classList.add('active');
  $searchModal.value = '';
  $searchModal.focus();
  renderSearchResults('');
}

function closeSearch() {
  $searchOverlay.classList.remove('active');
}

function renderSearchResults(query) {
  const q = query.toLowerCase().trim();

  const results = q === ''
    ? contentIndex.map(r => ({ ...r, matchType: 'browse' }))
    : contentIndex
        .map(r => {
          const titleMatch = r.title.toLowerCase().includes(q);
          const terms = q.split(/\s+/);
          const allTerms = terms.every(t => r.searchText.includes(t));
          const matchedHeading = r.headings.find(h => h.toLowerCase().includes(q));
          if (!titleMatch && !allTerms && !matchedHeading) return null;
          const matchType = titleMatch ? 'title' : (matchedHeading ? 'heading' : 'content');
          return { ...r, matchType, matchedHeading: matchedHeading || null };
        })
        .filter(Boolean)
        .sort((a, b) => {
          const order = { title: 0, heading: 1, content: 2 };
          return (order[a.matchType] ?? 3) - (order[b.matchType] ?? 3);
        });

  if (results.length === 0) {
    $searchResults.innerHTML = '<div class="search-empty">No results found</div>';
    return;
  }

  $searchResults.innerHTML = results.map(r => {
    let detail = '';
    if (r.matchType === 'heading' && r.matchedHeading) {
      detail = `<div class="search-result-match">§ ${r.matchedHeading}</div>`;
    } else if (r.matchType === 'content' && r.snippet) {
      detail = `<div class="search-result-match">${r.snippet.slice(0, 100)}…</div>`;
    }
    return `
      <div class="search-result" data-page="${r.page}" tabindex="0" role="option">
        <div class="search-result-title">${r.title}</div>
        ${r.section ? `<div class="search-result-section">${r.section}</div>` : ''}
        ${detail}
      </div>`;
  }).join('');

  $searchResults.querySelectorAll('.search-result').forEach(el => {
    const go = () => { closeSearch(); navigate(el.dataset.page); };
    el.addEventListener('click', go);
    el.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
  });
}

// ---------- Theme ----------
function initTheme() {
  const saved = localStorage.getItem('cagent-docs-theme');
  const theme = saved || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : null);
  if (theme) document.documentElement.setAttribute('data-theme', theme);
}

function toggleTheme() {
  const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', next);
  localStorage.setItem('cagent-docs-theme', next);
}

// ---------- Copy buttons ----------
function addCopyButtons() {
  $content.querySelectorAll('pre').forEach(pre => {
    if (pre.querySelector('.copy-btn')) return;
    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.textContent = 'Copy';
    btn.setAttribute('aria-label', 'Copy code to clipboard');
    btn.addEventListener('click', async () => {
      await navigator.clipboard.writeText(pre.querySelector('code')?.textContent || pre.textContent);
      btn.textContent = 'Copied!';
      btn.classList.add('copied');
      setTimeout(() => { btn.textContent = 'Copy'; btn.classList.remove('copied'); }, 2000);
    });
    pre.style.position = 'relative';
    pre.appendChild(btn);
  });
}

// ---------- Mobile sidebar toggle ----------
function toggleSidebar() {
  $sidebar.classList.toggle('open');
}

// ---------- Event listeners ----------
window.addEventListener('hashchange', () => navigate(location.hash.slice(1) || 'home'));

$searchInput?.addEventListener('click', openSearch);
$searchInput?.addEventListener('focus', openSearch);
$searchModal?.addEventListener('input', (e) => renderSearchResults(e.target.value));
$searchOverlay?.addEventListener('click', (e) => { if (e.target === $searchOverlay) closeSearch(); });

document.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault();
    $searchOverlay.classList.contains('active') ? closeSearch() : openSearch();
  }
  if (e.key === 'Escape') closeSearch();
});

// ---------- Init ----------
initTheme();
buildSidebar();
navigate(location.hash.slice(1) || 'home');
prefetchPagesForSearch();
