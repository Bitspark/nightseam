/**
 * Hold the cross-references between prose, source comments, and rendered HTML.
 * @see [File index and evidence map](README.md)
 * @see [Renderer](render.mjs)
 */
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join, posix } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { anchors, check, links } from '../../scripts/links.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const tracked = new Set(
  execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean),
);
const theoryFiles = [...tracked].filter((path) => path.startsWith('docs/theory/'));
const read = (path) => readFileSync(join(root, path), 'utf8').replaceAll('\r\n', '\n');
const pages = new Map(
  [...tracked]
    .filter((path) => path.endsWith('.md') || (path.startsWith('docs/theory/') && /\.(ts|mjs)$/.test(path)))
    .map((path) => [
      path,
      path.endsWith('.md')
        ? read(path)
        : read(path)
            .split('\n')
            .map((line) => (line.includes('@see ') ? line : ''))
            .join('\n'),
    ]),
);

test('theory prose and source-comment links resolve, including Markdown fragments', () => {
  const problems = check(pages, tracked).filter(({ page }) => page.startsWith('docs/theory/'));
  assert.deepEqual(problems, []);
  for (const path of theoryFiles.filter((file) => /\.(ts|mjs)$/.test(file))) {
    assert.ok(
      links(pages.get(path)).some(({ target }) => /\.md(?:#|$)/.test(target)),
      path + ' needs a prose backlink',
    );
  }
});

test('the theory index links every owned document, source, and configuration file', () => {
  const indexed = new Set(
    links(pages.get('docs/theory/README.md')).map(({ target }) =>
      posix.normalize(posix.join('docs/theory', target.split('#')[0])),
    ),
  );
  for (const path of theoryFiles) {
    if (path === 'docs/theory/README.md') continue;
    assert.ok(indexed.has(path), path + ' is missing from the theory index');
  }
});

test('the visual edition preserves heading anchors and resolves every navigation link', () => {
  const html = read('docs/theory/index.html').replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '');
  const ids = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map((match) => match[1]));
  for (const anchor of anchors(pages.get('docs/theory/foundations.md'))) {
    assert.ok(ids.has(anchor), 'Missing foundations heading in HTML: ' + anchor);
  }
  const outgoing = [];
  for (const [, href] of html.matchAll(/<a\b[^>]*\bhref="([^"]+)"/g)) {
    if (href.startsWith('#')) {
      assert.ok(ids.has(decodeURIComponent(href.slice(1))), 'Missing HTML target: ' + href);
    } else {
      outgoing.push('[link](' + href + ')');
    }
  }
  const withHtmlLinks = new Map(pages).set('docs/theory/index.html', outgoing.join('\n'));
  assert.deepEqual(
    check(withHtmlLinks, tracked).filter(({ page }) => page.endsWith('/index.html')),
    [],
  );
});
