import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { posix } from 'node:path';
import test from 'node:test';
import { anchors, links } from '../../scripts/links.mjs';

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

test('Markdown and source backlinks resolve to the same theory headings', async () => {
  function check(source, target) {
    if (/^[a-z][a-z\d+.-]*:/i.test(target)) return;
    const [path, fragment] = target.split('#');
    const name = path ? posix.normalize(posix.join(posix.dirname(source), path)) : source;
    if (!pages.has(name)) return;
    if (fragment) assert.ok(anchors(pages.get(name)).has(fragment), source + ': unknown heading ' + target);
  }
  for (const [name, markdown] of pages) {
    for (const link of links(markdown)) check(name, link.target);
  }
  for (const name of sources) {
    const references = [...(await read(name)).matchAll(/@see\s+(\S+)/g)];
    assert.ok(references.length > 0, name + ': missing theory backlink');
    for (const [, target] of references) {
      const page = posix.normalize(posix.join(posix.dirname(name), target.split('#')[0]));
      assert.ok(pages.has(page), name + ': unknown theory page ' + target);
      check(name, target);
    }
  }
});

test('the standalone guide has unique targets and no sibling-file dependencies', async () => {
  const html = (await read('index.html')).replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '');
  const identifiers = [...html.matchAll(/\bid="([^"]+)"/g)].map((match) => match[1]);
  const ids = new Set(identifiers);
  assert.equal(ids.size, identifiers.length, 'duplicate HTML identifiers');
  for (const [, href] of html.matchAll(/\bhref="([^"]+)"/g)) {
    if (href.startsWith('#')) assert.ok(ids.has(href.slice(1)), 'missing HTML target: ' + href);
    else assert.match(href, /^https:\/\//, 'link depends on the checkout: ' + href);
  }
  for (const [name, markdown] of pages) {
    const pageId = pageIds.get(name);
    assert.ok(ids.has(pageId), 'missing embedded page: ' + name);
    const headings = [...anchors(markdown)];
    for (const heading of headings.slice(1)) {
      const id = pageId === 'foundations' ? heading : pageId + '-' + heading;
      assert.ok(ids.has(id), 'missing embedded heading: ' + id);
    }
  }
  for (const name of sources) {
    assert.ok(ids.has('source-' + name.replace(/[^a-z0-9]+/g, '-')), 'missing embedded source: ' + name);
  }
});
