/**
 * Hold the cross-references between prose, source comments, and rendered HTML.
 * @see README.md — File index and evidence map
 * @see render.mjs — Renderer
 */
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs/promises';
import { posix } from 'node:path';
import test from 'node:test';
import { anchors, check as checkLinks, links } from '../../scripts/links.mjs';

const root = new URL('../../', import.meta.url);
const tracked = new Set(
  execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean),
);
const theoryFiles = [...tracked].filter((path) => path.startsWith('docs/theory/'));
const repositoryPages = new Map(
  await Promise.all(
    [...tracked]
      .filter((path) => path.endsWith('.md') || (path.startsWith('docs/theory/') && /\.(ts|mjs)$/.test(path)))
      .map(async (path) => {
        const source = await fs.readFile(new URL(path, root), 'utf8');
        return [
          path,
          path.endsWith('.md')
            ? source
            : source
                .split('\n')
                .map((line) => {
                  const target = line.match(/@see\s+(\S+)/)?.[1];
                  return target ? '[source backlink](' + target + ')' : '';
                })
                .join('\n'),
        ];
      }),
  ),
);

const pageIds = new Map([
  ['foundations.md', 'foundations'],
  ['reference.md', 'reference'],
  ['nightseam.md', 'nightseam'],
  ['README.md', 'overview'],
]);
const read = (name) => fs.readFile(new URL(name, import.meta.url), 'utf8');
const pages = new Map(await Promise.all([...pageIds.keys()].map(async (name) => [name, await read(name)])));
const sources = [
  'model.ts',
  'model.typecheck.ts',
  'examples/coordinates.ts',
  'examples/hierarchy.ts',
  'examples/composition.ts',
  'examples/behavior.ts',
];

test('the reference map covers exactly the exported model types', async () => {
  const model = await read('model.ts');
  const exported = new Set([...model.matchAll(/^export (?:type|interface) (\w+)/gm)].map((match) => match[1]));
  const documented = new Set(
    pages
      .get('reference.md')
      .split('\n')
      .filter((line) => line.startsWith('|'))
      .flatMap((line) => [...(line.split('|')[2] ?? '').matchAll(/`(\w+)`/g)].map((match) => match[1])),
  );
  assert.deepEqual(documented, exported);
  assert.deepEqual(Object.keys(await import('./model.ts')), [], 'the model contains types only');
});

test('the reference map covers every named law', () => {
  const laws = [...pages.get('foundations.md').matchAll(/^(?:\*\*)?([A-Z]\d+)(?: —|:)/gm)].map((match) => match[1]);
  const expanded = pages
    .get('reference.md')
    .replace(/([A-Z])(\d+)–\1(\d+)/g, (_, prefix, first, last) =>
      Array.from({ length: Number(last) - Number(first) + 1 }, (_, index) => prefix + (Number(first) + index)).join(
        ' ',
      ),
    );
  const documented = new Set([...expanded.matchAll(/\b([A-Z]\d+)\b/g)].map((match) => match[1]));
  assert.ok(laws.length > 0);
  assert.deepEqual(documented, new Set(laws));
});

test('theory prose and source backlinks resolve, including Markdown fragments', () => {
  const problems = checkLinks(repositoryPages, tracked).filter(({ page }) => page.startsWith('docs/theory/'));
  assert.deepEqual(problems, []);
  for (const path of theoryFiles.filter((file) => /\.(ts|mjs)$/.test(file))) {
    assert.ok(
      links(repositoryPages.get(path)).some(({ target }) => /\.md(?:#|$)/.test(target)),
      path + ': missing prose backlink',
    );
  }
});

test('the theory index links every owned document, source, and configuration file', () => {
  const indexed = new Set(
    links(pages.get('README.md')).map(({ target }) => posix.normalize(posix.join('docs/theory', target.split('#')[0]))),
  );
  for (const path of theoryFiles) {
    if (path === 'docs/theory/README.md') continue;
    assert.ok(indexed.has(path), path + ': missing from the theory index');
  }
});

test('the standalone guide has unique targets and no sibling-file dependencies', async () => {
  const html = (await read('index.html')).replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '');
  const identifiers = [...html.matchAll(/\bid="([^"]+)"/g)].map((match) => match[1]);
  const ids = new Set(identifiers);
  assert.equal(ids.size, identifiers.length, 'duplicate HTML identifiers');
  const outgoing = [];
  for (const [, href] of html.matchAll(/\bhref="([^"]+)"/g)) {
    if (href.startsWith('#')) assert.ok(ids.has(href.slice(1)), 'missing HTML target: ' + href);
    else {
      assert.match(href, /^https:\/\//, 'link depends on the checkout: ' + href);
      outgoing.push('[link](' + href + ')');
    }
  }
  const withHtmlLinks = new Map(repositoryPages).set('docs/theory/index.html', outgoing.join('\n'));
  assert.deepEqual(
    checkLinks(withHtmlLinks, tracked).filter(({ page }) => page === 'docs/theory/index.html'),
    [],
  );
  for (const [name, markdown] of pages) {
    const pageId = pageIds.get(name);
    assert.ok(ids.has(pageId), 'missing embedded page: ' + name);
    const headings = [...anchors(markdown)];
    for (const heading of pageId === 'foundations' ? headings : headings.slice(1)) {
      const id = pageId === 'foundations' ? heading : pageId + '-' + heading;
      assert.ok(ids.has(id), 'missing embedded heading: ' + id);
    }
  }
  for (const name of sources) {
    assert.ok(ids.has('source-' + name.replace(/[^a-z0-9]+/g, '-')), 'missing embedded source: ' + name);
  }
});
