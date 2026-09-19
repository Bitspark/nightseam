// Thin DOM layer; the builders above also run in Node without a browser.
const atlas = buildAtlas(JSON.parse(document.getElementById('nightseam-document').textContent));
const main = document.getElementById('content');
const drawer = document.getElementById('drawer');
const searchDialog = document.getElementById('finder');
const lensSelect = document.getElementById('lens');
let currentFamily = '';
let breadcrumbs = [];
let lens = 'wire';
let returnFocus = null;

for (const language of atlas.languages) {
  const option = document.createElement('option');
  option.value = language;
  option.textContent = language;
  lensSelect.append(option);
}
lensSelect.disabled = atlas.languages.length === 0;
if (lensSelect.disabled) lensSelect.title = 'This document contains wire names only.';

function navigate({ force = false } = {}) {
  // Section links stay in the current family and do not rebuild its view.
  const section = ['#board', '#vocabulary', '#errors', '#across', '#content'].includes(location.hash);
  if (section && !force) return;
  const parts = section ? [currentFamily] : parseRoute(location.hash || '#/');
  const name = parts?.[0] || atlas.families[0]?.Name;
  const view = familyView(atlas, name, lens);
  document.getElementById('families').innerHTML = atlas.families
    .map((f) => link(route(f.Name), f.Name, f.Name === name ? 'aria-current="page"' : ''))
    .join('');
  if (!view) {
    main.innerHTML = `<p class="empty">${atlas.families.length ? 'This route is not in the checkout. Choose a family above.' : 'This checkout has no families.'}</p>`;
    drawer.close();
    return;
  }
  const open = new Set([...main.querySelectorAll('details.exchange[open]')].map((e) => e.id));
  const focusHref = document.activeElement?.getAttribute('href');
  if (name !== currentFamily) breadcrumbs = [];
  currentFamily = name;
  main.innerHTML = familyHTML(atlas, name, lens);
  for (const id of open) {
    const el = document.getElementById(id);
    if (el) el.open = true;
  }
  if (parts?.[1] === 'type' && view.types.some((t) => t.Name === parts[2])) {
    const href = route(name, 'type', parts[2]);
    const existing = breadcrumbs.findIndex((b) => b.href === href);
    if (existing >= 0) breadcrumbs = breadcrumbs.slice(0, existing + 1);
    else breadcrumbs.push({ href, name: parts[2] });
    document.getElementById('breadcrumbs').innerHTML =
      link(route(name), name) + ' / ' + breadcrumbs.map((b) => link(b.href, b.name)).join(' / ');
    document.getElementById('type-detail').innerHTML = typeHTML(atlas, name, parts[2], lens);
    drawer.scrollTop = 0;
    if (!drawer.open) {
      returnFocus = [...main.querySelectorAll('a')].find((a) => a.getAttribute('href') === focusHref) || main;
      drawer.showModal();
    }
    return;
  }
  if (drawer.open) drawer.close();
  if (parts?.length > 1) {
    const id = anchor(...parts);
    const target = document.getElementById(id);
    if (target) {
      if (target.tagName === 'DETAILS') target.open = true;
      target.scrollIntoView({ block: 'start' });
    } else main.insertAdjacentHTML('afterbegin', '<p role="status" class="empty">That item is not in this family.</p>');
  } else if (!section) window.scrollTo({ top: 0 });
}

function search() {
  const results = finder(atlas, document.getElementById('search').value);
  document.getElementById('results').innerHTML = results.length
    ? results
        .map(
          (e) =>
            `<a class="search-result" href="${esc(e.href)}"><span>${esc(e.name)}</span><small>${esc(e.family)} · ${esc(e.kind)}</small></a>`,
        )
        .join('')
    : '<p class="empty">No matching names or descriptions.</p>';
}
function openFinder() {
  search();
  searchDialog.showModal();
  document.getElementById('search').focus();
}
document.getElementById('find').addEventListener('click', openFinder);
document.getElementById('search').addEventListener('input', search);
document.getElementById('search').addEventListener('keydown', (event) => {
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    document.querySelector('#results a')?.focus();
  }
  if (event.key === 'Enter') document.querySelector('#results a')?.click();
});
document.getElementById('results').addEventListener('keydown', (event) => {
  if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return;
  const entries = [...document.querySelectorAll('#results a')];
  const index = entries.indexOf(document.activeElement);
  const next = entries[index + (event.key === 'ArrowDown' ? 1 : -1)];
  if (next) {
    event.preventDefault();
    next.focus();
  }
});
document.addEventListener('keydown', (event) => {
  if (
    event.key === '/' &&
    !event.ctrlKey &&
    !event.metaKey &&
    !['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName) &&
    !drawer.open &&
    !searchDialog.open
  ) {
    event.preventDefault();
    openFinder();
  }
});
document.addEventListener('click', (event) => {
  const close = event.target.closest('[data-close]');
  if (close) document.getElementById(close.dataset.close).close();
  const a = event.target.closest('a[href^="#/"]');
  if (!a || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  if (searchDialog.open) searchDialog.close();
  if (a.getAttribute('href') === location.hash) {
    event.preventDefault();
    navigate();
  }
});
drawer.addEventListener('close', () => {
  if (parseRoute(location.hash)?.[1] === 'type') {
    history.replaceState(null, '', route(currentFamily));
    returnFocus?.focus();
  }
});
lensSelect.addEventListener('change', () => {
  lens = lensSelect.value;
  navigate({ force: true });
});
document.getElementById('theme').addEventListener('click', () => {
  const root = document.documentElement;
  const dark = root.dataset.theme ? root.dataset.theme === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
  root.dataset.theme = dark ? 'light' : 'dark';
});
window.addEventListener('hashchange', navigate);
navigate();
