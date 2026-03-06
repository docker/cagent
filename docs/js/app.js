/* ===================================================
   cagent docs – Jekyll site utilities
   (theme, search, TOC, copy buttons)
   =================================================== */

// ---------- DOM references ----------
const $content      = document.getElementById('page-content');
const $searchInput  = document.getElementById('search-input');
const $searchOverlay = document.getElementById('search-overlay');
const $searchModal   = document.getElementById('search-modal-input');
const $searchResults = document.getElementById('search-results');

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

// ---------- Table of Contents ----------
function slugify(text) {
  return text.toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

function buildTOC() {
  if (!$content) return;

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
          return `<a class="toc-link ${level}" href="#${h.id}" data-id="${h.id}">${h.textContent}</a>`;
        }).join('')}
      </nav>
    </div>`;

  const main = document.querySelector('.main');
  if (main) main.appendChild(aside);

  aside.addEventListener('click', (e) => {
    const link = e.target.closest('.toc-link');
    if (!link) return;
    e.preventDefault();
    const target = document.getElementById(link.dataset.id);
    if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });

  setupScrollSpy(headings, aside);
}

function setupScrollSpy(headings, aside) {
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        aside.querySelectorAll('.toc-link').forEach(l => l.classList.remove('active'));
        aside.querySelector(`.toc-link[data-id="${entry.target.id}"]`)?.classList.add('active');
      }
    }
  }, { rootMargin: '-80px 0px -70% 0px', threshold: 0 });

  headings.forEach(h => observer.observe(h));
}

// ---------- Copy buttons ----------
function addCopyButtons() {
  if (!$content) return;
  // Target both direct <pre> and Rouge-generated pre.highlight
  $content.querySelectorAll('pre, pre.highlight').forEach(pre => {
    if (pre.querySelector('.copy-btn')) return;
    // Skip if this pre is inside a .highlight div that already has a button
    if (pre.closest('.highlight')?.querySelector('.copy-btn')) return;
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

// ---------- Search ----------
// Build a search index from sidebar links and page content
const searchIndex = [];

function buildSearchIndex() {
  // Index from sidebar navigation
  document.querySelectorAll('.sidebar-link').forEach(link => {
    const section = link.closest('.sidebar-section')?.querySelector('.sidebar-heading')?.textContent || '';
    searchIndex.push({
      title: link.textContent.trim(),
      url: link.getAttribute('href'),
      section: section,
      searchText: `${link.textContent} ${section}`.toLowerCase(),
    });
  });

  // Also index current page content for richer results
  if ($content) {
    const currentEntry = searchIndex.find(e => {
      const currentPath = window.location.pathname;
      return currentPath.endsWith(e.url) || currentPath.endsWith(e.url.replace(/\/$/, ''));
    });
    if (currentEntry) {
      currentEntry.searchText += ' ' + $content.textContent.replace(/\s+/g, ' ').toLowerCase();
    }
  }
}

function openSearch() {
  $searchOverlay?.classList.add('active');
  if ($searchModal) {
    $searchModal.value = '';
    $searchModal.focus();
  }
  renderSearchResults('');
}

function closeSearch() {
  $searchOverlay?.classList.remove('active');
}

function renderSearchResults(query) {
  if (!$searchResults) return;
  const q = query.toLowerCase().trim();

  const results = q === ''
    ? searchIndex.map(r => ({ ...r, matchType: 'browse' }))
    : searchIndex
        .map(r => {
          const titleMatch = r.title.toLowerCase().includes(q);
          const terms = q.split(/\s+/);
          const allTerms = terms.every(t => r.searchText.includes(t));
          if (!titleMatch && !allTerms) return null;
          const matchType = titleMatch ? 'title' : 'content';
          return { ...r, matchType };
        })
        .filter(Boolean)
        .sort((a, b) => {
          const order = { title: 0, content: 1 };
          return (order[a.matchType] ?? 2) - (order[b.matchType] ?? 2);
        });

  if (results.length === 0) {
    $searchResults.innerHTML = '<div class="search-empty">No results found</div>';
    return;
  }

  $searchResults.innerHTML = results.map(r => `
    <a class="search-result" href="${r.url}" tabindex="0" role="option">
      <div class="search-result-title">${r.title}</div>
      ${r.section ? `<div class="search-result-section">${r.section}</div>` : ''}
    </a>
  `).join('');

  $searchResults.querySelectorAll('.search-result').forEach(el => {
    el.addEventListener('click', () => closeSearch());
  });
}

// ---------- Event listeners ----------
$searchInput?.addEventListener('click', openSearch);
$searchInput?.addEventListener('focus', openSearch);
$searchModal?.addEventListener('input', (e) => renderSearchResults(e.target.value));
$searchOverlay?.addEventListener('click', (e) => { if (e.target === $searchOverlay) closeSearch(); });

document.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault();
    $searchOverlay?.classList.contains('active') ? closeSearch() : openSearch();
  }
  if (e.key === 'Escape') closeSearch();
});

// ---------- Init ----------
initTheme();
buildSearchIndex();
buildTOC();
addCopyButtons();
