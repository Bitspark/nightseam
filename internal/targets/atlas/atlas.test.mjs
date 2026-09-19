import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  buildAtlas,
  familyView,
  finder,
  route,
  parseRoute,
  annotate,
  typeText,
  shapeKey,
  escapeHTML,
  familyHTML,
  typeHTML,
} from './atlas.mjs';

const scalar = { Name: 'Text', Kind: 'alias', Alias: 'string', Example: 'hello' };
const checkout = {
  Families: [
    {
      Name: 'alpha',
      Types: [
        scalar,
        {
          Name: 'Node',
          Kind: 'record',
          Open: true,
          Example: { text: 'hello', next: null },
          Fields: [
            { Name: 'text', Declared: 'Text', Required: true, Description: 'Human text.' },
            { Name: 'next', Declared: { nullable: 'Node' }, Required: false },
          ],
          UsedBy: [{ Side: 'server', Kind: 'method', Name: 'read', At: 'result' }],
        },
        {
          Name: 'Choice',
          Kind: 'union',
          Tag: 'kind',
          Value: 'body',
          Variants: [
            { Tag: 'empty', Empty: true, Example: { kind: 'empty' } },
            { Tag: 'node', Declared: 'Node', Example: { kind: 'node', body: { text: 'hello', next: null } } },
            { Tag: 'record', Declared: { kind: 'record', fields: [] }, Example: { kind: 'record', body: {} } },
          ],
          Example: { kind: 'empty' },
        },
      ],
      Server: {
        Methods: [
          {
            Name: 'read',
            DeclaredResult: 'Node',
            Weight: { Request: 0, Result: 2 },
            Frames: { Request: { method: 'read' }, Response: { result: { text: 'hello' } } },
          },
        ],
        Events: [],
      },
      Client: { Methods: [], Events: [{ Name: 'changed', Declared: 'Node', Weight: 2, Frame: { event: 'changed' } }] },
      Errors: [{ Code: 'missing', Description: 'Not here.' }],
    },
    { Name: 'beta', Types: [{ ...scalar, Description: 'Another description.' }], Errors: [{ Code: 'missing' }] },
  ],
};

test('every finder route resolves to a family-view anchor, with distinct sides and kinds', () => {
  const atlas = buildAtlas(checkout);
  const ids = new Set(atlas.families.flatMap((f) => familyView(atlas, f.Name).ids));
  for (const entry of finder(atlas)) {
    assert(ids.has(entry.id), entry.id);
    assert.equal(route(...parseRoute(entry.href)), entry.href);
  }
  assert.notEqual(route('alpha', 'method', 'server', 'same'), route('alpha', 'method', 'client', 'same'));
  assert.equal(parseRoute('#/%ZZ'), null);
  assert.deepEqual(parseRoute(route('a b', 'type', 'A/B#C')), ['a b', 'type', 'A/B#C']);
});

test('exchange direction follows the initiator, and weights come from the document', () => {
  const view = familyView(buildAtlas(checkout), 'alpha');
  assert.deepEqual(
    view.exchanges.map((x) => [x.name, x.initiator, x.weight]),
    [
      ['read', 'client', 2],
      ['changed', 'client', 2],
    ],
  );
  assert.deepEqual(view.exchanges[0].frames, checkout.Families[0].Server.Methods[0].Frames);
  assert.deepEqual(
    finder(buildAtlas(checkout), 'not here').map((x) => x.name),
    ['missing'],
  );
});

test('annotations retain presence and descriptions, fold recursion, and expose open records', () => {
  const atlas = buildAtlas(checkout);
  const row = annotate(atlas, 'alpha', 'Node', checkout.Families[0].Types[1].Example);
  assert.equal(row.children[0].presence, 'required');
  assert.equal(row.children[0].description, 'Human text.');
  assert.equal(row.children[1].presence, 'optional');
  assert.equal(row.children[1].recursive, true);
  assert.equal(row.children.at(-1).ghost, true);
  assert.equal(row.children[1].value, null);
});

test('every union arm keeps its canonical example and the configured payload member', () => {
  const atlas = buildAtlas(checkout);
  const row = annotate(atlas, 'alpha', 'Choice', checkout.Families[0].Types[2].Example);
  assert.deepEqual(
    row.variants.map((v) => v.tag),
    ['empty', 'node', 'record'],
  );
  assert.equal(row.variants[0].children.length, 1);
  assert.equal(row.variants[1].children[1].name, 'body');
  assert.deepEqual(row.variants[2].children[1].value, {});
});

test('type spellings describe all expression forms without language knowledge', () => {
  assert.equal(
    typeText({ apply: 'Page', with: { T: { array: { nullable: 'string' } } } }),
    'Page<T = (string | null)[]>',
  );
  assert.equal(typeText({ map: { literal: 'x' } }), 'map<string, "x">');
  assert.equal(typeText({ ref: 'Node' }), 'key of Node');
  assert.equal(typeText('S.Envelope'), 'S.Envelope');
});

test('shape comparison ignores prose and wire type names, but holds constraints and presence', () => {
  const atlas = buildAtlas(checkout);
  assert.equal(shapeKey(atlas, 'alpha', 'Text'), shapeKey(atlas, 'beta', 'Text'));
  const changed = structuredClone(checkout);
  changed.Families[1].Types[0].Alias = 'integer';
  assert.notEqual(shapeKey(buildAtlas(changed), 'alpha', 'Text'), shapeKey(buildAtlas(changed), 'beta', 'Text'));
  assert.equal(atlas.shared.length, 1);
  assert.equal(atlas.shared[0].same, true);
});

test('a lens never changes routes, payloads, ordering, or weights', () => {
  const doc = structuredClone(checkout);
  doc.Families[0].Types[0].Languages = { future: { Name: 'FutureText', Declare: 'type FutureText = String' } };
  doc.Families[0].Server.Methods[0].Languages = {
    future: { Name: 'Read', Invoke: { Call: 'client.Read()', Handle: '' } },
  };
  const atlas = buildAtlas(doc);
  assert.deepEqual(familyView(atlas, 'alpha', 'unavailable'), familyView(atlas, 'alpha', 'wire'));
  const wire = familyView(atlas, 'alpha');
  const future = familyView(atlas, 'alpha', 'future');
  assert.equal(future.types[0].display, 'FutureText');
  assert.deepEqual(
    finder(atlas, 'FutureText').map((entry) => entry.name),
    ['Text'],
  );
  assert.equal(future.exchanges[0].display, 'Read');
  future.types[0].display = wire.types[0].display;
  future.exchanges[0].display = wire.exchanges[0].display;
  assert.deepEqual(future, wire);
  assert(typeHTML(atlas, 'alpha', 'Text', 'future').includes('type FutureText = String'));
  const rendered = familyHTML(atlas, 'alpha', 'future');
  assert(rendered.includes('client.Read()'));
  assert(!rendered.includes('<h3>Handle</h3>'));
  assert.equal(escapeHTML('<script a="x">&\''), '&lt;script a=&quot;x&quot;&gt;&amp;&#39;');
});

test('rendered anchors cover the finder and hostile prose stays text', () => {
  const doc = structuredClone(checkout);
  doc.Families[0].Types[0].Description = '</script><img src="x" onerror="boom">';
  const atlas = buildAtlas(doc);
  const html = atlas.families.map((f) => familyHTML(atlas, f.Name)).join('');
  for (const entry of finder(atlas)) assert(html.includes(`id="${escapeHTML(entry.id)}"`), entry.id);
  const sections = [...html.matchAll(/href="(#[^"]+\/section\/[^\"]+)"/g)];
  assert.equal(sections.length, 4 * atlas.families.length);
  for (const [, href] of sections) {
    assert(atlas.families.some((f) => f.Name === parseRoute(href)[0]));
    assert(html.includes(`id="${href.slice(1)}"`));
  }
  assert(!html.includes('<img'));
  assert(html.includes('&lt;img'));
});

test(
  "the generator's proof document annotates settled forms without losing examples",
  { skip: !process.env.ATLAS_PROOF },
  () => {
    const doc = JSON.parse(readFileSync(process.env.ATLAS_PROOF, 'utf8'));
    const atlas = buildAtlas(doc);
    const f = atlas.families.find((f) => f.Name === 'proof');
    assert(f);
    const part = f.Types.find((t) => t.Name === 'Part');
    const annotated = annotate(atlas, f.Name, part.Name, part.Example);
    assert.equal(annotated.variants.length, part.Variants.length);
    const page = f.Types.find((t) => t.Name === 'Page');
    assert(annotate(atlas, f.Name, page.Name, page.Example).children.some((r) => r.type.includes('null')));
    assert(f.Types.some((t) => t.Inline));
    const parts = f.Types.find((t) => t.Name === 'Parts');
    assert.equal(annotate(atlas, f.Name, parts.Name, parts.Example).children[0].children[0].kind, 'union');
    assert(typeHTML(atlas, f.Name, parts.Name).includes('Alias of Page&lt;T = Part&gt;.'));
    const ids = new Set(atlas.families.flatMap((f) => familyView(atlas, f.Name).ids));
    for (const entry of finder(atlas)) assert(ids.has(entry.id), entry.id);
    for (const v of annotated.variants) assert(v.example !== undefined, v.tag);
    const result = annotate(
      atlas,
      f.Name,
      { apply: 'Result', with: { T: 'Parts', E: 'string' } },
      { kind: 'err', value: 'example' },
    );
    const alternate = result.variants.find((v) => v.tag === 'ok');
    assert(alternate.declarationExample);
    assert.equal(alternate.children[1].type, 'string');
    assert.equal(alternate.children[1].children.length, 0);
  },
);

test('example bindings and unavailable reasons are visible and escaped', () => {
  const doc = structuredClone(checkout);
  const type = doc.Families[0].Types[0];
  type.ExampleBindings = { T: { Type: 'string' }, S: { Family: 'alpha' } };
  let atlas = buildAtlas(doc);
  let html = typeHTML(atlas, 'alpha', 'Text');
  assert(html.includes('T = string') && html.includes('S = family alpha'));
  type.Example = null;
  type.ExampleUnavailable = { Kind: 'limit', Reason: '<budget exhausted>' };
  const op = doc.Families[0].Server.Methods[0];
  op.ExampleBindings = type.ExampleBindings;
  op.ExampleUnavailable = type.ExampleUnavailable;
  op.Frames = {};
  atlas = buildAtlas(doc);
  html = typeHTML(atlas, 'alpha', 'Text');
  assert(html.includes('Example unavailable (limit): &lt;budget exhausted&gt;'));
  assert(!html.includes('<pre><code>null'));
  const page = familyHTML(atlas, 'alpha');
  assert(page.includes('Example unavailable (limit): &lt;budget exhausted&gt;'));
  assert(page.includes('S = family alpha'));
});
