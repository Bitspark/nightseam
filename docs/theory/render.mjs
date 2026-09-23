import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import { Script } from 'node:vm';
import { marked } from 'marked';
import * as hierarchy from './examples/hierarchy.ts';
import * as composition from './examples/composition.ts';
import * as behavior from './examples/behavior.ts';

const directory = import.meta.dirname;
const escape = (value) =>
  String(value).replace(
    /[&<>"']/g,
    (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
  );
const slug = (value) =>
  value
    .replace(/^\d+\.\s*/, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
const files = [
  'model.ts',
  'model.typecheck.ts',
  'examples/coordinates.ts',
  'examples/hierarchy.ts',
  'examples/composition.ts',
  'examples/behavior.ts',
];
const sources = await Promise.all(
  files.map(async (name) => ({
    name,
    content: (await fs.readFile(`${directory}/${name}`, 'utf8')).replaceAll('\r\n', '\n'),
  })),
);
const markdown = (await fs.readFile(`${directory}/foundations.md`, 'utf8')).replaceAll('\r\n', '\n');
let document = marked.parse(markdown).replace(/^<h1>.*?<\/h1>\s*/, '');
const contents = [];
document = document.replace(/<h2>(.*?)<\/h2>/g, (_, title) => {
  const id = slug(title.replace(/<[^>]*>/g, ''));
  contents.push({ id, title });
  return `<h2 id="${id}">${title}</h2>`;
});
for (const name of files) document = document.replaceAll(`href="${name}"`, `href="#source-${slug(name)}"`);
document = document.replaceAll('href="index.html"', 'href="#top"');
document = document.replaceAll('<table>', '<div class="table-wrap"><table>').replaceAll('</table>', '</table></div>');

const behaviorFigure = `<figure id="behavior-figure" class="diagram">
  <figcaption><span class="figure-number">BEHAVIOR EXPLORER</span><strong>Lawful does not mean the same behavior.</strong></figcaption>
  <p>Compare a storing cell initially true with one of the checked implementations. A replacing adapter returns a lawful cell, but changes the first read.</p>
  <label class="path-label" for="behavior-case">Implementation to compare</label>
  <select id="behavior-case">${behavior.cases.map((item, index) => `<option value="${index}"${index === 3 ? ' selected' : ''}>${escape(item.name)}</option>`).join('')}</select>
  <p id="behavior-verdict" class="figure-result" aria-live="polite"></p>
  <div class="route-controls" role="group" aria-label="Cell interactions">
    <button type="button" data-cell-input="0">Read</button>
    <button type="button" data-cell-input="1">Write false</button>
    <button type="button" data-cell-input="2">Write true</button>
    <button type="button" id="cell-reset">Reset</button>
  </div>
  <div class="artifact-grid"><div><h3>Original true cell</h3><pre id="original-trace" aria-live="polite">No interactions yet.</pre></div><div><h3>Selected implementation</h3><pre id="selected-trace" aria-live="polite">No interactions yet.</pre></div></div>
  <p class="figure-result">The verdict concerns all finite traces from each initial state. The controls show individual traces. Exhaustively checked across ${behavior.checkedMachines} finite method implementations; no transport or concurrency is modeled.</p>
</figure>`;
assert.ok(document.includes('<!-- behavior-explorer -->'));
document = document.replace('<!-- behavior-explorer -->', behaviorFigure);

function drawSquare(kind, width) {
  const w = Math.max(250, Math.round(width));
  const narrow = w < 430;
  const x = 8,
    gap = narrow ? 46 : 116,
    cw = (w - 2 * x - gap) / 2;
  const xr = w - x - cw,
    top = 36,
    bottom = 218,
    h = 68;
  const leftMid = x + cw / 2,
    rightMid = xr + cw / 2;
  const isLaw = kind === 'transparency';
  const isComposition = kind === 'composition';
  const nodes = isLaw
    ? [
        ['Root at K', 'R_K(S)'],
        ['Root at L', 'R_L(S)'],
        ['Part at K', 'R_K(S|p)'],
        ['Part at L', 'R_L(S|p)'],
      ]
    : isComposition
      ? [
          ['Open at K', 'r, a(g)'],
          ['Open at L', 'F(r), F(a)'],
          ['Filled at K', 'R_K(S[σ])'],
          ['Filled at L', 'R_L(S[σ])'],
        ]
      : [
          ['Nested', 'ascending'],
          ['Flat', 'ascending'],
          ['Nested', 'descending'],
          ['Flat', 'descending'],
        ];
  const labels = isLaw
    ? ['F_S', 'select p', 'select p', 'F_(S|p)']
    : isComposition
      ? ['F each', 'fill', 'fill', 'F instance']
      : ['flatten', 'reorder', 'reorder', 'flatten'];
  const route = (key, d) => `<path data-branch="${key}" class="edge" d="${d}" marker-end="url(#arrow-${kind})"/>`;
  const node = (index, nx, ny) =>
    `<g class="graph-node"><rect x="${nx}" y="${ny}" width="${cw}" height="${h}" rx="10"/><text class="node-title" x="${nx + cw / 2}" y="${ny + 27}" text-anchor="middle">${nodes[index][0]}</text><text class="node-detail" x="${nx + cw / 2}" y="${ny + 49}" text-anchor="middle">${nodes[index][1]}</text></g>`;
  const edgeLabel = (text, nx, ny, anchor = 'middle') =>
    `<text class="edge-label" x="${nx}" y="${ny}" text-anchor="${anchor}">${text}</text>`;
  return `<svg viewBox="0 0 ${w} 312" width="${w}" height="312" role="img" aria-labelledby="title-${kind} desc-${kind}">
    <title id="title-${kind}">${isLaw ? 'Navigation commutes with transformation' : isComposition ? 'Substitution commutes with transformation' : 'Two coordinate routes to the same representation'}</title>
    <desc id="desc-${kind}">${isLaw ? 'Transforming the whole and then selecting path p agrees with selecting p first and transforming that sub-shape.' : isComposition ? 'Transform the template and each argument, then fill; or fill first and transform the resulting instance. Both routes represent S with the same substituted arguments at the target coordinate.' : 'Flatten then reorder, or reorder then flatten. Each edge changes one axis. Both routes produce the same canonical flat descending encoding in this example.'}</desc>
    <defs><marker id="arrow-${kind}" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="context-stroke"/></marker></defs>
    ${route('first', `M ${x + cw + 6} ${top + h / 2} H ${xr - 8}`)}
    ${route('first', `M ${rightMid} ${top + h + 8} V ${bottom - 10}`)}
    ${route('second', `M ${leftMid} ${top + h + 8} V ${bottom - 10}`)}
    ${route('second', `M ${x + cw + 6} ${bottom + h / 2} H ${xr - 8}`)}
    ${node(0, x, top)}${node(1, xr, top)}${node(2, x, bottom)}${node(3, xr, bottom)}
    ${edgeLabel(labels[0], w / 2, top + 16)}
    ${edgeLabel(labels[3], w / 2, bottom + h + 19)}
    ${edgeLabel(labels[1], rightMid, top + h + 70)}
    ${edgeLabel(labels[2], leftMid, top + h + 70)}
    <text class="equivalence" x="${w / 2}" y="171" text-anchor="middle">≈</text>
  </svg>`;
}

function allPaths(shape, prefix = []) {
  return [prefix, ...[...shape.children].flatMap(([key, child]) => allPaths(child, [...prefix, key]))];
}
const samples = allPaths(hierarchy.booleanCellTree).map((path) => {
  const selection = hierarchy.locate(hierarchy.booleanCellTree, path);
  assert.equal(selection.kind, 'found');
  const part = hierarchy.nestedNavigation()(hierarchy.rootRepresentation, selection.value);
  const transformThenSelect = hierarchy.flatNavigation()(
    hierarchy.flattenAscending(hierarchy.rootRepresentation),
    selection.value,
  );
  const selectThenTransform = hierarchy.flattenAscending(part);
  assert.deepEqual(transformThenSelect.artifact, selectThenTransform.artifact);
  const firstRoute = hierarchy.runLayoutFirst(part);
  const secondRoute = hierarchy.runOrderFirst(part);
  assert.deepEqual(firstRoute.artifact, secondRoute.artifact);
  return {
    path,
    label: path.length ? path.join(' / ') : '[] — whole shape',
    nested: part.artifact,
    flat: transformThenSelect.artifact,
    flatDescending: firstRoute.artifact,
  };
});
const choices = samples
  .map(
    (sample, index) =>
      `<option value="${index}"${sample.path.join('/') === 'operations/write/arguments' ? ' selected' : ''}>${escape(sample.label)}</option>`,
  )
  .join('');

const lawFigure = `<figure id="transparency-figure" class="diagram" data-route="first">
  <figcaption><span class="figure-number">FIGURE 1</span><strong>One selected part. Two routes.</strong></figcaption>
  <label class="path-label" for="shape-path">Shape path <code>p</code></label>
  <select id="shape-path">${choices}</select>
  <div class="route-controls" role="group" aria-label="Route through the transparency square">
    <button type="button" data-route-choice="first" aria-pressed="true">Transform → select</button>
    <button type="button" data-route-choice="second" aria-pressed="false">Select → transform</button>
  </div>
  <div class="square" data-square="transparency">${drawSquare('transparency', 700)}</div>
  <p id="selection-result" class="figure-result" aria-live="polite">The selected subtree is preserved along both routes.</p>
  <details class="artifact-details"><summary>Inspect the selected artifacts</summary><div class="artifact-grid"><div><h3>Nested at K</h3><pre id="nested-artifact"></pre></div><div><h3>Flat at L · either route</h3><pre id="flat-artifact"></pre></div></div></details>
</figure>`;
const coordinateFigure = `<figure id="coordinate-figure" class="diagram" data-route="first">
  <figcaption><span class="figure-number">FIGURE 2</span><strong>Two coordinate paths</strong></figcaption>
  <div class="route-controls" role="group" aria-label="Order of coordinate transformations">
    <button type="button" data-route-choice="first" aria-pressed="true">Layout first</button>
    <button type="button" data-route-choice="second" aria-pressed="false">Order first</button>
  </div>
  <div class="square" data-square="coordinates">${drawSquare('coordinates', 700)}</div>
  <p class="figure-result" aria-live="polite">Flatten → reorder. The other route produces the same canonical artifact in this example.</p>
</figure>`;
const compositionFigure = `<figure id="composition-figure" class="diagram" data-route="first">
  <figcaption><span class="figure-number">FIGURE 4</span><strong>Filling commutes with transformation</strong></figcaption>
  <div class="route-controls" role="group" aria-label="Route through the substitution square">
    <button type="button" data-route-choice="first" aria-pressed="true">Transform → fill</button>
    <button type="button" data-route-choice="second" aria-pressed="false">Fill → transform</button>
  </div>
  <div class="square" data-square="composition">${drawSquare('composition', 700)}</div>
  <p class="figure-result" aria-live="polite">Transform the template and every argument, then fill. Both routes represent the same instance S[σ].</p>
</figure>`;

function shapeTree(root) {
  const label = (node) => (node.kind === 'generic' ? '?' + node.id : (node.value ?? '(no local value)'));
  const lines = [label(root)];
  function visit(node, prefix) {
    if (node.kind === 'generic') return;
    const entries = [...node.children];
    entries.forEach(([key, child], index) => {
      const last = index === entries.length - 1;
      lines.push(prefix + (last ? '└─ ' : '├─ ') + key + ' → ' + label(child));
      visit(child, prefix + (last ? '   ' : '│  '));
    });
  }
  visit(root, '');
  return lines.join('\n');
}
const stages = [
  {
    title: 'Template',
    tree: shapeTree(composition.pair),
    description: 'Pair[g0]: the same parameter occurs at first and second.',
  },
  {
    title: 'Insert List',
    tree: shapeTree(composition.pairOfLists),
    description: 'g0 := List[h0]: both holes receive an entire List shape, including its element child.',
  },
  {
    title: 'Insert Bool',
    tree: shapeTree(composition.closedPair),
    description: 'h0 := Bool: both lists are now closed shapes.',
  },
];
const stageFigure = `<figure id="composition-stages" class="diagram"><figcaption><span class="figure-number">FIGURE 3</span><strong>A whole shape at each hole</strong></figcaption><div class="route-controls" role="group" aria-label="Substitution stage">${stages.map((stage, index) => `<button type="button" data-stage-choice="${index}" aria-pressed="${index === 0}">${stage.title}</button>`).join('')}</div><pre id="composition-tree">${escape(stages[0].tree)}</pre><p id="composition-stage-result" class="figure-result" aria-live="polite">${escape(stages[0].description)}</p></figure>`;
const treeFence = /<pre><code class="language-text">Pair\[g0\][\s\S]*?<\/code><\/pre>/;
assert.ok(treeFence.test(document));
document = document.replace(treeFence, stageFigure);
let diagrams = 0;
document = document.replace(
  /<pre><code class="language-mermaid">[\s\S]*?<\/code><\/pre>/g,
  () => [lawFigure, coordinateFigure, compositionFigure][diagrams++],
);
assert.equal(diagrams, 3);

const appendix = sources
  .map(
    ({ name, content }) =>
      `<details class="source" id="source-${slug(name)}"><summary><span>${escape(name)}</span><span class="source-note">${content.split(/\r?\n/).length} lines · embedded source</span></summary><pre><code>${escape(content)}</code></pre></details>`,
  )
  .join('\n');
const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>Contracts, implementations, and representations</title>
<style>
:root{color-scheme:light dark;--paper:#f6f5f0;--surface:#fffefb;--ink:#203337;--muted:#536a6c;--line:#d3ddda;--accent:#167263;--wash:#e9f3ed;--code:#edf1ed;--focus:#3158a9;--shadow:rgba(22,51,45,.05)}
@media(prefers-color-scheme:dark){:root{--paper:#111c1f;--surface:#18272b;--ink:#e5eeeb;--muted:#b3c7c4;--line:#3a5155;--accent:#82d2bf;--wash:#203b38;--code:#213337;--focus:#b5c5fa;--shadow:rgba(0,0,0,.1)}}
*{box-sizing:border-box}html{scroll-behavior:smooth;scroll-padding-top:26px}body{margin:0;background:var(--paper);color:var(--ink);font:16px/1.7 'Segoe UI',system-ui,sans-serif}a{color:var(--accent);text-underline-offset:3px}a:hover{text-decoration-thickness:2px}a:focus-visible,button:focus-visible,select:focus-visible,summary:focus-visible{outline:3px solid var(--focus);outline-offset:4px}button,select{font:inherit;color:inherit}button{cursor:pointer}p{margin:1em 0}strong{font-weight:600}h1,h2,h3{font-weight:600;line-height:1.22;letter-spacing:-.02em}h1{font-size:clamp(36px,5vw,62px);margin:14px 0 20px;max-width:850px}h2{font-size:27px;margin:70px 0 22px;padding-top:24px;border-top:1px solid var(--line)}h3{font-size:17px;margin:18px 0 12px}code,pre{font-family:Consolas,'Cascadia Code',monospace}code{font-size:.9em;background:var(--code);padding:.12em .28em;border-radius:4px;overflow-wrap:anywhere}pre{background:var(--code);padding:18px 20px;border-radius:9px;font-size:13.5px;line-height:1.65;white-space:pre-wrap;overflow-wrap:anywhere}pre code{font:inherit;background:none;padding:0;border-radius:0}blockquote{margin:24px 0;border-left:3px solid var(--accent);padding:2px 20px;color:var(--muted)}
.shell{max-width:1220px;margin:auto;padding:48px 40px 64px}.hero{padding:0 0 34px;border-bottom:1px solid var(--line)}.eyebrow{font-size:12px;font-weight:600;letter-spacing:.13em;color:var(--muted)}.subtitle{font-size:20px;line-height:1.55;max-width:700px;color:var(--muted)}.hero-links{display:flex;gap:22px;flex-wrap:wrap;margin-top:24px;font-size:14px}.layout{display:grid;grid-template-columns:225px minmax(0,1fr);gap:48px}aside{padding-top:44px}.toc{position:sticky;top:28px;max-height:calc(100vh - 56px);overflow-y:auto;padding-right:8px;font-size:13px;line-height:1.45}.toc summary{font-weight:600;margin-bottom:14px}.toc nav{display:grid;gap:4px}.toc a{color:var(--muted);text-decoration:none;padding:7px 0}.toc a:hover{color:var(--accent)}article{min-width:0}article>h2:first-child{margin-top:22px;border-top:0}.table-wrap{width:100%;margin:24px 0}table{width:100%;border-collapse:collapse;font-size:14px;line-height:1.55;table-layout:fixed}th{text-align:left;font-weight:600;border-bottom:2px solid var(--line);padding:12px 10px}td{padding:12px 10px;vertical-align:top;border-bottom:1px solid var(--line);overflow-wrap:anywhere}th:first-child,td:first-child{width:36%;padding-left:0}th:last-child,td:last-child{padding-right:0}ol,ul{padding-left:24px}
.diagram{margin:30px 0;padding:24px;background:var(--surface);border:1px solid var(--line);border-radius:14px;box-shadow:0 4px 20px var(--shadow)}figcaption{display:flex;flex-direction:column;gap:6px;line-height:1.4;margin-bottom:22px}figcaption strong{font-size:20px}.figure-number{font-size:11px;letter-spacing:.13em;color:var(--muted);font-weight:600}.path-label{display:block;font-size:13px;font-weight:600;margin-bottom:6px}select{width:100%;max-width:100%;background:var(--surface);border:1px solid var(--line);border-radius:6px;padding:10px;font-size:14px;min-height:44px}.route-controls{display:flex;gap:8px;flex-wrap:wrap;margin:16px 0 4px}.route-controls button{border:1px solid var(--line);background:var(--surface);padding:8px 12px;border-radius:6px;font-size:13px;min-height:40px}.route-controls button[aria-pressed=true]{background:var(--ink);color:var(--surface);border-color:var(--ink)}.square{width:100%;min-width:0}.square svg{display:block;width:100%;height:auto;overflow:visible}.graph-node rect{fill:var(--wash);stroke:var(--line);stroke-width:1}.square text{fill:var(--ink);font:14px 'Segoe UI',system-ui,sans-serif}.square .node-title{font-weight:600}.square .node-detail{font:13px Consolas,monospace;fill:var(--muted)}.square .edge{stroke:var(--line);stroke-width:2;fill:none}.square .edge-label{font-size:12px;paint-order:stroke;stroke:var(--surface);stroke-width:7px;stroke-linejoin:round}.square .equivalence{font-size:30px;fill:var(--muted)}.diagram[data-route=first] [data-branch=first],.diagram[data-route=second] [data-branch=second]{stroke:var(--accent);stroke-width:3}.figure-result{font-size:14px;border-top:1px solid var(--line);padding-top:16px;margin:10px 0 0}.artifact-details{margin-top:18px}.artifact-details summary{font-size:14px}.artifact-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:14px}.artifact-grid pre{font-size:12px;padding:12px;line-height:1.55}.artifact-grid h3{font-size:14px}.artifact-grid>div{min-width:0}details summary{cursor:pointer}.source{margin:16px 0;padding:18px 0;border-bottom:1px solid var(--line)}.source summary{font-size:15px;font-weight:600}.source-note{font-size:12px;color:var(--muted);font-weight:400;display:block;padding-left:18px;margin-top:4px}.source pre{font-size:12px}.footer{margin-top:54px;padding-top:20px;border-top:1px solid var(--line);font-size:13px;color:var(--muted)}
@media(max-width:900px){.shell{padding:32px 24px 48px}.layout{grid-template-columns:180px minmax(0,1fr);gap:28px}.diagram{padding:18px}h2{font-size:24px}.toc{font-size:12px}}
@media(max-width:700px){.shell{padding:26px 18px 40px}.layout{display:block}aside{padding-top:22px}.toc{position:static;max-height:none;overflow:visible;border-bottom:1px solid var(--line);padding-bottom:18px;font-size:14px}.toc summary{margin-bottom:0}.toc[open] summary{margin-bottom:12px}.toc a{padding:8px 0}h2{margin-top:48px;font-size:24px}body{font-size:15.5px}.subtitle{font-size:18px}.diagram{margin:24px -3px;padding:14px}figcaption strong{font-size:18px}.artifact-grid{grid-template-columns:1fr}.route-controls{gap:6px}.route-controls button{font-size:12px;padding:8px;min-height:44px}pre{padding:14px 12px;font-size:12px}table{font-size:13px}td,th{padding:10px 7px}th:first-child,td:first-child{width:37%}select{font-size:16px}}
@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}}
@media print{:root{color-scheme:light;--paper:white;--surface:white;--ink:#182a2d;--muted:#42595c;--line:#b9c8c4;--accent:#116959;--wash:#eef6f1;--code:#f2f5f3}body{font-size:10pt}.shell{padding:0;max-width:none}.layout{display:block}aside,.hero-links,.route-controls,select,.path-label,.artifact-details,.source{display:none}h1{font-size:32pt}h2{font-size:18pt;margin-top:28pt;break-after:avoid}h3{break-after:avoid}pre{font-size:8pt;break-inside:avoid}table{font-size:9pt}tr,.diagram{break-inside:avoid}.diagram{box-shadow:none;padding:14pt}a{color:inherit}p{orphans:3;widows:3}.footer{font-size:9pt}}
</style></head><body><div class="shell" id="top"><header class="hero"><div class="eyebrow">NIGHTSEAM THEORY · CONTRACTS AND REPRESENTATIONS</div><h1>Contracts, behavior,<br>and representations.</h1><p class="subtitle">What a contract permits, what an implementation does, and what must survive a change of representation.</p><div class="hero-links"><a href="#behavior-figure">Explore behavior ↓</a><a href="#transformation-families-and-the-transparency-square">Compare transformation routes ↓</a><a href="#whole-shape-holes">Compose shapes ↓</a><a href="#embedded-sources">Read the model sources ↓</a></div></header><div class="layout"><aside><details class="toc" open><summary>In this document</summary><nav aria-label="Contents">${contents.map(({ id, title }) => `<a href="#${id}">${title}</a>`).join('')}<a href="#embedded-sources">Embedded sources</a></nav></details></aside><article>${document}<h2 id="embedded-sources">Embedded sources</h2><p>The complete model, compile-only assertions, and all four examples are included below. The diagrams, interactions, styles, and sources work offline.</p>${appendix}<footer class="footer">TypeScript as metalanguage · Generated from foundations.md and the model sources</footer></article></div></div>
<script>
const samples = ${JSON.stringify(samples).replaceAll('<', '\\u003c')};
const stages = ${JSON.stringify(stages).replaceAll('<', '\\u003c')};
const behaviorCases = ${JSON.stringify(behavior.cases).replaceAll('<', '\\u003c')};
const behaviorInputs = ${JSON.stringify(behavior.inputs).replaceAll('<', '\\u003c')};
const behaviorPicker = document.getElementById('behavior-case');
let originalState;
let selectedState;
let originalTrace;
let selectedTrace;
function resetBehavior() {
  const selected = behaviorCases[Number(behaviorPicker.value)];
  originalState = behaviorCases[1].initial;
  selectedState = selected.initial;
  originalTrace = [];
  selectedTrace = [];
  document.getElementById('original-trace').textContent = 'No interactions yet.';
  document.getElementById('selected-trace').textContent = 'No interactions yet.';
  document.getElementById('behavior-verdict').textContent = 'Satisfies the cell contract: ' + (selected.lawful ? 'yes' : 'no') + '. Same behavior as the original true cell: ' + (selected.sameAsTrueCell ? 'yes' : 'no') + '.';
}
behaviorPicker.addEventListener('change', resetBehavior);
document.getElementById('cell-reset').addEventListener('click', resetBehavior);
document.querySelectorAll('[data-cell-input]').forEach(button => button.addEventListener('click', () => {
  const index = Number(button.dataset.cellInput);
  const input = behaviorInputs[index];
  const label = input.kind === 'read' ? 'read' : 'write(' + input.value + ')';
  const selected = behaviorCases[Number(behaviorPicker.value)];
  const originalNext = behaviorCases[1].transitions.find(row => row.state === originalState).steps[index];
  const selectedNext = selected.transitions.find(row => row.state === selectedState).steps[index];
  const output = next => next.output.kind === 'read' ? String(next.output.value) : 'done';
  originalTrace.push(label + ' → ' + output(originalNext));
  selectedTrace.push(label + ' → ' + output(selectedNext));
  originalState = originalNext.state;
  selectedState = selectedNext.state;
  document.getElementById('original-trace').textContent = originalTrace.join('\\n');
  document.getElementById('selected-trace').textContent = selectedTrace.join('\\n');
}));
resetBehavior();
const drawSquare = ${drawSquare.toString()};
const picker = document.getElementById('shape-path');
const lawFigure = document.getElementById('transparency-figure');
function updateSelection() {
  const sample = samples[Number(picker.value)];
  const route = lawFigure.dataset.route === 'first' ? 'Transform → select' : 'Select → transform';
  const count = sample.flat.nodes.length;
  document.getElementById('selection-result').textContent = route + ': ' + count + (count === 1 ? ' node' : ' nodes') + ' preserved at “' + sample.label + '”. Both routes agree.';
  document.getElementById('nested-artifact').textContent = JSON.stringify(sample.nested, null, 2);
  document.getElementById('flat-artifact').textContent = JSON.stringify(sample.flat, null, 2);
}
picker.addEventListener('change', updateSelection);
document.querySelectorAll('.diagram').forEach(figure => {
  figure.querySelectorAll('[data-route-choice]').forEach(button => button.addEventListener('click', () => {
    figure.dataset.route = button.dataset.routeChoice;
    figure.querySelectorAll('[data-route-choice]').forEach(other => other.setAttribute('aria-pressed', String(other === button)));
    if (figure === lawFigure) updateSelection();
    else if (figure.id === 'composition-figure') figure.querySelector('.figure-result').textContent = (figure.dataset.route === 'first' ? 'Transform the template and every argument, then fill.' : 'Fill the template, then transform the resulting instance.') + ' Both routes represent the same instance S[σ].';
    else figure.querySelector('.figure-result').textContent = (figure.dataset.route === 'first' ? 'Flatten → reorder.' : 'Reorder → flatten.') + ' The other route produces the same canonical artifact in this example.';
  }));
});
document.querySelectorAll('[data-stage-choice]').forEach(button => button.addEventListener('click', () => {
  const stage = stages[Number(button.dataset.stageChoice)];
  document.getElementById('composition-tree').textContent = stage.tree;
  document.getElementById('composition-stage-result').textContent = stage.description;
  document.querySelectorAll('[data-stage-choice]').forEach(other => other.setAttribute('aria-pressed', String(other === button)));
}));
document.querySelectorAll('[data-square]').forEach(container => {
  const render = () => { container.innerHTML = drawSquare(container.dataset.square, container.clientWidth); };
  new ResizeObserver(render).observe(container);
  render();
});
if (matchMedia('(max-width:700px)').matches) document.querySelector('.toc').open = false;
function revealSource() {
  const element = document.getElementById(location.hash.slice(1));
  if (element && element.classList.contains('source')) element.open = true;
}
window.addEventListener('hashchange', revealSource);
updateSelection();
revealSource();
</script></body></html>`;
const browserScript = html.match(/<script>([\s\S]*)<\/script>/)[1];
assert.ok(!/https?:\/\//.test(browserScript));
new Script(browserScript); // Reject malformed generated JavaScript before browser verification.
const finalHtml = html;
const output = `${directory}/index.html`;
if (process.argv.slice(2).some((argument) => argument !== '--check')) {
  throw new Error('Usage: node --experimental-strip-types render.mjs [--check]');
}
if (process.argv.includes('--check')) {
  let existing;
  try {
    existing = (await fs.readFile(output, 'utf8')).replaceAll('\r\n', '\n');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
  assert.equal(
    existing === finalHtml,
    true,
    'docs/theory/index.html is missing or stale; run pnpm --filter @nightseam/theory render',
  );
  console.log('Theory HTML is current.');
} else {
  await fs.writeFile(output, finalHtml, 'utf8');
  console.log('Rendered docs/theory/index.html (' + Buffer.byteLength(finalHtml) + ' bytes).');
}
