// Pure views of doc.Checkout. Values, frames and language spellings belong
// to the document; this writer only selects, annotates and presents them.
export const list = (v) => v ?? [];
export const escapeHTML = (v) =>
  String(v ?? '').replace(
    /[&<>"']/g,
    (c) =>
      ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
      })[c],
  );
export const route = (...parts) => '#/' + parts.map(encodeURIComponent).join('/');
export function parseRoute(hash) {
  if (!hash.startsWith('#/')) return null;
  try {
    return hash.slice(2).split('/').map(decodeURIComponent);
  } catch {
    return null;
  }
}
export const anchor = (...parts) => route(...parts).slice(1);
const allTypes = (f) => [...list(f.Types), ...list(f.Carried)];
const lookup = (atlas, family, name) => atlas.types.get(family)?.get(name);
const qualified = (family, name) => (name.includes('.') ? name.split('.') : [family, name]);

export function typeText(expr) {
  if (expr == null) return 'nothing';
  if (typeof expr === 'string') return expr;
  if (expr.array) return `${expr.array.nullable ? `(${typeText(expr.array)})` : typeText(expr.array)}[]`;
  if (expr.map) return `map<string, ${typeText(expr.map)}>`;
  if (expr.nullable) return `${typeText(expr.nullable)} | null`;
  if (Object.hasOwn(expr, 'literal')) return JSON.stringify(expr.literal);
  if (expr.ref) return `key of ${expr.ref}`;
  if (expr.apply)
    return `${expr.apply}<${Object.entries(expr.with ?? {})
      .map(([k, v]) => `${k} = ${typeText(v)}`)
      .join(', ')}>`;
  return expr.kind ?? 'value';
}

// Bind a displayed expression through an application. A binding keeps the
// lexical family of its filler, including imported and nested applications.
function resolve(atlas, family, expr, bindings = new Map()) {
  if (typeof expr === 'string' && bindings.has(expr)) {
    const b = bindings.get(expr);
    return resolve(atlas, b.family, b.expr, b.bindings);
  }
  if (typeof expr === 'string' && expr.includes('.')) {
    const [param, name] = expr.split('.');
    if (bindings.has(param)) {
      const b = bindings.get(param);
      return resolve(atlas, b.expr, name, b.bindings);
    }
  }
  if (expr?.apply) {
    const [owner, name] = qualified(family, expr.apply);
    const next = new Map(bindings);
    for (const [p, value] of Object.entries(expr.with ?? {})) next.set(p, { family, expr: value, bindings });
    return { family: owner, type: lookup(atlas, owner, name), expr, bindings: next };
  }
  if (typeof expr === 'string') {
    const [owner, name] = qualified(family, expr);
    return { family: owner, type: lookup(atlas, owner, name), expr, bindings };
  }
  return { family, expr, bindings };
}

export function buildAtlas(checkout) {
  const families = list(checkout.Families);
  const atlas = {
    families,
    types: new Map(families.map((f) => [f.Name, new Map(allTypes(f).map((t) => [t.Name, t]))])),
    shared: [],
    languages: [],
  };
  const languages = new Set();
  const names = new Map();
  for (const f of families) {
    for (const t of allTypes(f)) {
      Object.keys(t.Languages ?? {}).forEach((l) => languages.add(l));
      if (!t.Carried) {
        if (!names.has(t.Name)) names.set(t.Name, []);
        names.get(t.Name).push({ family: f.Name, type: t });
      }
    }
    for (const side of [f.Server, f.Client]) {
      for (const op of [...list(side?.Methods), ...list(side?.Events)])
        Object.keys(op.Languages ?? {}).forEach((l) => languages.add(l));
    }
  }
  atlas.languages = [...languages].sort();
  for (const [name, members] of names) {
    if (members.length < 2) continue;
    const shapes = members.map((m) => shapeKey(atlas, m.family, m.type.Name));
    atlas.shared.push({ name, members, same: shapes.every((s) => s === shapes[0]) });
  }
  atlas.shared.sort((a, b) => a.name.localeCompare(b.name));
  return atlas;
}

export function familyView(atlas, name, lens = 'wire') {
  const f = atlas.families.find((f) => f.Name === name);
  if (!f) return null;
  const exchanges = [];
  for (const [sideName, side] of [
    ['server', f.Server],
    ['client', f.Client],
  ]) {
    for (const [kind, ops] of [
      ['method', side?.Methods],
      ['event', side?.Events],
    ]) {
      for (const op of list(ops)) {
        const initiator = kind === 'event' ? sideName : sideName === 'server' ? 'client' : 'server';
        // A side receives calls to its methods and emits its own events.
        const path = [name, kind, sideName, op.Name];
        exchanges.push({
          id: anchor(...path),
          href: route(...path),
          name: op.Name,
          display: op.Languages?.[lens]?.Name || op.Name,
          kind,
          side: sideName,
          initiator,
          description: op.Description ?? '',
          op,
          frames: kind === 'method' ? op.Frames : { Event: op.Frame },
          weight: kind === 'method' ? (op.Weight?.Request ?? 0) + (op.Weight?.Result ?? 0) : (op.Weight ?? 0),
        });
      }
    }
  }
  exchanges.sort((a, b) => (a.initiator === 'client' ? 0 : 1) - (b.initiator === 'client' ? 0 : 1));
  const types = allTypes(f).map((t) => ({
    ...t,
    display: t.Languages?.[lens]?.Name || t.Name,
    id: anchor(name, 'type', t.Name),
    href: route(name, 'type', t.Name),
  }));
  const errors = list(f.Errors).map((e) => ({
    ...e,
    id: anchor(name, 'error', e.Code),
    href: route(name, 'error', e.Code),
    also: atlas.families
      .filter((other) => other.Name !== name && list(other.Errors).some((x) => x.Code === e.Code))
      .map((f) => f.Name),
  }));
  return {
    family: f,
    id: anchor(name),
    exchanges,
    types,
    errors,
    ids: [anchor(name), ...exchanges.map((x) => x.id), ...types.map((t) => t.id), ...errors.map((e) => e.id)],
  };
}

export function finder(atlas, query = '') {
  const entries = [];
  for (const f of atlas.families) {
    const v = familyView(atlas, f.Name);
    entries.push({
      name: f.Name,
      family: f.Name,
      kind: 'family',
      id: v.id,
      href: route(f.Name),
      description: f.Source,
    });
    for (const x of v.exchanges) entries.push({ ...x, family: f.Name });
    for (const t of v.types)
      entries.push({
        name: t.Name,
        family: f.Name,
        kind: 'type',
        id: t.id,
        href: t.href,
        description: t.Description,
        aliases: Object.values(t.Languages ?? {})
          .map((language) => language.Name)
          .filter(Boolean),
      });
    for (const e of v.errors)
      entries.push({ name: e.Code, family: f.Name, kind: 'error', id: e.id, href: e.href, description: e.Description });
  }
  const words = query.toLowerCase().trim().split(/\s+/);
  return entries.filter((e) =>
    words.every((w) =>
      `${e.family} ${e.name} ${e.kind} ${e.description ?? ''} ${list(e.aliases).join(' ')}`.toLowerCase().includes(w),
    ),
  );
}

// Each annotation has the supplied example value. Missing optional fields
// remain absent, never invented. Recursion is stopped by declaration identity.
export function annotate(atlas, family, expression, value, options = {}) {
  const { trail = [], bindings = new Map(), name = '', presence = '', description = '' } = options;
  const row = { name, type: typeText(expression), value, presence, description, children: [] };
  const r = resolve(atlas, family, expression, bindings);
  row.type = typeText(r.expr);
  const child = (expr, value, extra = {}) =>
    annotate(atlas, r.family, expr, value, { trail, bindings: r.bindings, ...extra });
  if (r.type) {
    const t = r.type;
    row.kind = t.Kind;
    row.href = route(r.family, 'type', t.Name);
    const identity = `${r.family}.${t.Name}`;
    if (trail.includes(identity)) {
      row.recursive = true;
      return row;
    }
    const next = [...trail, identity];
    if (t.Kind === 'alias') {
      const target = child(t.Alias, value, { trail: next });
      return {
        ...target,
        ...row,
        kind: target.kind,
        children: target.children,
        variants: target.variants,
        recursive: target.recursive,
      };
    }
    if (t.Kind === 'record' || t.Kind === 'entity') {
      row.children = list(t.Fields).map((field) =>
        child(
          field.Nullable && !(field.Declared ?? field.Type)?.nullable
            ? { nullable: field.Declared ?? field.Type }
            : (field.Declared ?? field.Type),
          value?.[field.Name],
          {
            trail: next,
            name: field.Name,
            presence: field.Required ? 'required' : 'optional',
            description: field.Description ?? '',
          },
        ),
      );
      if (t.Open)
        row.children.push({
          name: '…',
          type: 'json',
          ghost: true,
          presence: 'optional',
          description: 'Additional members are preserved.',
          children: [],
        });
    }
    if (t.Kind === 'union') {
      row.variants = list(t.Variants).map((v) => {
        // Variant examples are built by doc, including the envelope. A
        // selected arm can also be annotated in a concrete application.
        const selected = value?.[t.Tag] === v.Tag;
        const example = selected ? value : v.Example;
        const declarationExample = !selected && r.bindings.size > 0;
        const tag = { name: t.Tag, type: JSON.stringify(v.Tag), value: v.Tag, presence: 'required', children: [] };
        const children = [tag];
        if (!v.Empty)
          children.push(
            child(v.Declared ?? v.Type, example?.[t.Value], {
              trail: next,
              name: t.Value,
              presence: 'required',
              // The alternate example belongs to the generic declaration.
              // Only the selected value was instantiated by doc's exampler.
              bindings: declarationExample ? new Map() : r.bindings,
            }),
          );
        return { tag: v.Tag, example, children, declarationExample };
      });
    }
    if (t.Kind === 'enum') row.values = list(t.Values);
    return row;
  }
  const expr = r.expr;
  if (expr?.nullable) {
    const target = child(expr.nullable, value);
    return {
      ...target,
      ...row,
      kind: target.kind,
      children: target.children,
      variants: target.variants,
      recursive: target.recursive,
      href: target.href,
    };
  }
  if (expr?.array) {
    row.kind = 'array';
    row.children = (Array.isArray(value) ? value : []).map((v, i) => child(expr.array, v, { name: String(i) }));
  }
  if (expr?.map) {
    row.kind = 'map';
    row.children = Object.entries(value && typeof value === 'object' ? value : {}).map(([name, v]) =>
      child(expr.map, v, { name }),
    );
  }
  return row;
}

// Shape is structural: prose, declaration origin and generated language
// names do not participate; wire members, constraints and variants do.
export function shapeKey(atlas, family, expression) {
  function shape(family, expr, trail = [], bindings = new Map()) {
    const r = resolve(atlas, family, expr, bindings);
    const child = (e, next = trail) => shape(r.family, e, next, r.bindings);
    const t = r.type;
    if (t) {
      const id = `${r.family}.${t.Name}`;
      if (trail.includes(id)) return ['recursive', trail.indexOf(id)];
      const next = [...trail, id];
      if (t.Kind === 'alias') return child(t.Alias, next);
      if (t.Kind === 'enum') return ['enum', [...list(t.Values)].sort()];
      if (t.Kind === 'union')
        return [
          'union',
          t.Tag,
          t.Value,
          [...list(t.Variants)]
            .sort((a, b) => a.Tag.localeCompare(b.Tag))
            .map((v) => [v.Tag, v.Empty ? 'empty' : child(v.Declared ?? v.Type, next)]),
        ];
      return [
        t.Kind,
        !!t.Open,
        t.Key,
        [...list(t.Fields)]
          .sort((a, b) => a.Name.localeCompare(b.Name))
          .map((f) => [
            f.Name,
            !!f.Required,
            !!f.Nullable,
            !!f.Unique,
            f.Min,
            f.Max,
            f.Length,
            f.Pattern,
            child(f.Declared ?? f.Type, next),
          ]),
      ];
    }
    const e = r.expr;
    if (e && typeof e === 'object') {
      if (e.array) return ['array', child(e.array)];
      if (e.map) return ['map', child(e.map)];
      if (e.nullable) return ['nullable', child(e.nullable)];
      if (e.ref) return ['ref', child(e.ref)];
    }
    return e;
  }
  return JSON.stringify(shape(family, expression));
}

const esc = escapeHTML;
const link = (href, label, attributes = '') => `<a href="${esc(href)}" ${attributes}>${esc(label)}</a>`;
const json = (value) => (value === undefined ? 'absent' : JSON.stringify(value, null, 2));
const code = (value) => `<pre><code>${esc(value)}</code></pre>`;
const chip = (text) => `<span class="chip">${esc(text)}</span>`;
const refRoute = (family, ref) =>
  ref.Kind === 'type' ? route(family, 'type', ref.Name) : route(family, ref.Kind, ref.Side, ref.Name);

export function annotationHTML(row, depth = 0) {
  const type = row.href ? link(row.href, row.type) : esc(row.type);
  const value =
    row.value && typeof row.value === 'object'
      ? Array.isArray(row.value)
        ? `[${row.value.length} items]`
        : '{…}'
      : json(row.value);
  const line = `<div class="annotated-row${row.ghost ? ' ghost' : ''}"><span class="field">${esc(row.name || 'value')} <span class="value">${row.ghost ? '' : esc(value)}</span></span><span class="meaning">${type}${row.presence ? ` · ${esc(row.presence)}` : ''}${row.recursive ? ' · recursive ↩' : ''}</span>${row.description ? `<span class="field-description">${esc(row.description)}</span>` : ''}</div>`;
  const children = list(row.children)
    .map((c) => annotationHTML(c, depth + 1))
    .join('');
  const variants = list(row.variants)
    .map(
      (v) =>
        `<div class="variant"><span class="small-label">${esc(v.tag)}${v.declarationExample ? ' · declaration example' : ''}</span>${v.children.map((c) => annotationHTML(c, depth + 1)).join('')}</div>`,
    )
    .join('');
  if (!children && !variants) return line;
  return `${line}<details class="nested"${depth === 0 ? ' open' : ''}><summary>${variants ? `${row.variants.length} variants` : `${row.children.filter((c) => !c.ghost).length} members`}</summary>${children}${variants}</details>`;
}

function parametersHTML(parameters) {
  if (!list(parameters).length) return '';
  return `<div class="parameters"><span class="small-label">Parameters</span>${parameters.map((p) => `<p><strong>${esc(p.Name)}</strong> · ${esc(p.Of || 'type')}${p.Description ? ` — ${esc(p.Description)}` : ''}${list(p.Drawn).length ? `<br><span class="references">Draws ${esc(p.Drawn.join(', '))}</span>` : ''}</p>`).join('')}</div>`;
}

function frameHTML(atlas, family, title, speaker, frame, expr, member) {
  if (frame == null) return '';
  return `<div class="frame"><h4 class="${speaker}"><span class="dot"></span>${esc(title)}</h4>${code(json(frame))}${expr ? `<div class="annotation">${annotationHTML(annotate(atlas, family, expr, frame[member], { name: member }))}</div>` : ''}</div>`;
}

function exchangeHTML(atlas, f, x, lens) {
  const op = x.op;
  const opposite = x.initiator === 'client' ? 'server' : 'client';
  const session = f.Session;
  const governance = [
    x.kind === 'method' && x.side === 'server' && list(session?.Decides).includes(x.name) ? 'decides' : '',
    x.kind === 'method' && x.side === 'client' && list(session?.Asks).includes(x.name) ? 'asks' : '',
    x.kind === 'event' && x.side === 'server' && session?.Conversation?.Event === x.name
      ? `conversation at ${session.Conversation.Path}`
      : '',
  ].filter(Boolean);
  const names = [
    chip(`wire: ${x.name}`),
    ...Object.entries(op.Languages ?? {})
      .filter(([, lang]) => lang.Name)
      .map(([name, lang]) => chip(`${name}: ${lang.Name}`)),
  ].join('');
  const result = x.kind === 'method' ? typeText(op.DeclaredResult) : typeText(op.Declared);
  const left =
    x.initiator === 'client'
      ? `<span class="name client">${esc(x.display)}</span>`
      : `<span class="response-label">${esc(result)}</span>`;
  const right =
    x.initiator === 'server'
      ? `<span class="name server">${esc(x.display)}</span>`
      : `<span class="response-label">${esc(result)}</span>`;
  const frames =
    x.kind === 'event'
      ? frameHTML(atlas, f.Name, `${x.initiator} → ${opposite} · event`, x.initiator, op.Frame, op.Declared, 'data')
      : frameHTML(
          atlas,
          f.Name,
          `${x.initiator} → ${opposite} · request`,
          x.initiator,
          op.Frames?.Request,
          op.DeclaredRequest,
          'params',
        ) +
        frameHTML(
          atlas,
          f.Name,
          `${opposite} → ${x.initiator} · response`,
          opposite,
          op.Frames?.Response,
          op.DeclaredResult,
          'result',
        );
  const refusals = list(op.Frames?.Refusals)
    .map((r) => frameHTML(atlas, f.Name, `refusal · ${r.Code}`, opposite, r.Frame))
    .join('');
  const invocation = op.Languages?.[lens]?.Invoke;
  return `<details class="exchange" id="${esc(x.id)}"><summary>${left}<span class="direction"><span>${x.initiator === 'client' ? '→' : '←'} ${esc(x.kind)}</span></span>${right}</summary><div class="anatomy"><p class="description">${esc(x.description)}</p><div class="chips">${names}${governance.map(chip).join('')}${op.Origin?.Family && op.Origin.Family !== f.Name ? chip(`from ${op.Origin.Family}`) : ''}${link(x.href, 'Permalink', 'class="chip"')}</div>${list(op.Errors).length ? `<p class="references">Can fail with ${op.Errors.map((e) => link(route(f.Name, 'error', e), e)).join(' · ')}</p>` : ''}<div class="frames">${frames}</div>${refusals ? `<h3 class="declaration">Failures on the wire</h3><div class="frames">${refusals}</div>` : ''}${invocation ? `<div class="frames declaration">${invocation.Call ? `<div><h3>Call</h3>${code(invocation.Call)}</div>` : ''}${invocation.Handle ? `<div><h3>Handle</h3>${code(invocation.Handle)}</div>` : ''}</div>` : ''}</div></details>`;
}

export function familyHTML(atlas, name, lens = 'wire') {
  const v = familyView(atlas, name, lens);
  if (!v) return `<p class="empty">This family is not in the checkout.</p>`;
  const f = v.family;
  const count = v.exchanges.length;
  const max = Math.max(1, ...v.exchanges.map((x) => x.weight));
  const hall = v.exchanges
    .map((x) =>
      link(
        x.href,
        '',
        `class="${x.initiator}" title="${esc(x.name)} · ${x.weight} values" aria-label="${esc(x.name)}"`,
      ).replace('></a>', `><i style="height:${12 + Math.round((52 * x.weight) / max)}px"></i></a>`),
    )
    .join('');
  const board = ['client', 'server']
    .map((side) => {
      const exchanges = v.exchanges.filter((x) => x.initiator === side);
      return exchanges.length
        ? `<div class="group-label ${side}">${side} begins · ${exchanges.length}</div>${exchanges.map((x) => exchangeHTML(atlas, f, x, lens)).join('')}`
        : '';
    })
    .join('');
  const vocabulary = v.types
    .map(
      (t) =>
        `<article class="vocab-row" id="${esc(t.id)}">${link(t.href, t.display)}<div><p>${esc(t.Description)}</p><div class="chips">${chip(t.Kind)}${t.Inline ? chip('derived inline') : ''}${t.Carried ? chip(`carried from ${t.From}`) : ''}${list(
          t.Parameters,
        )
          .map((p) => chip(`${p.Name}: ${p.Of || 'type'}`))
          .join(
            '',
          )}</div>${list(t.UsedBy).length ? `<div class="references">Used by ${t.UsedBy.map((ref) => link(refRoute(name, ref), `${ref.Name} · ${ref.At}`)).join('')}</div>` : ''}</div></article>`,
    )
    .join('');
  const errors = v.errors
    .map(
      (e) =>
        `<article class="error-row" id="${esc(e.id)}">${link(e.href, e.Code)}<div><p>${esc(e.Description)}</p><div class="references">${v.exchanges
          .filter((x) => list(x.op.Errors).includes(e.Code))
          .map((x) => link(x.href, x.name))
          .join(
            '',
          )}${e.also.length ? `Also in ${e.also.map((n) => link(route(n, 'error', e.Code), n)).join('')}` : ''}</div></div></article>`,
    )
    .join('');
  const shared = atlas.shared
    .map(
      (s) =>
        `<tr><td>${esc(s.name)}</td><td>${s.members.map((m) => link(route(m.family, 'type', s.name), m.family)).join('')}</td><td>${s.same ? 'same shape' : 'different shapes'}</td></tr>`,
    )
    .join('');
  return `<div id="${esc(v.id)}" class="hero"><div><span class="eyebrow">Protocol atlas / ${esc(f.Builtin ? 'built-in family' : 'family')}</span><h1>${esc(f.Name)}<span class="client">.</span></h1><p class="lede">${f.Protocol ? 'What each side says, what it carries, and the vocabulary they share.' : 'The shared vocabulary, its shapes and the values they carry.'}</p><div class="facts"><span><strong>${count}</strong> exchanges</span><span><strong>${v.types.length}</strong> types</span><span><strong>${v.errors.length}</strong> errors</span></div></div><div><div class="legend"><span class="client"><i class="dot"></i>Client / lamp</span><span class="server"><i class="dot"></i>Server / moon</span></div><div class="hall" aria-label="Exchanges weighted by example values">${hall || '<span class="small-label">A vocabulary without exchanges</span>'}</div><p class="hall-caption">One mark per exchange. Height follows the values it carries.</p></div></div><nav class="section-nav" aria-label="Sections"><a href="${esc(route(name, 'section', 'board'))}">01 Exchanges</a><a href="${esc(route(name, 'section', 'vocabulary'))}">02 Vocabulary</a><a href="${esc(route(name, 'section', 'errors'))}">03 Errors</a><a href="${esc(route(name, 'section', 'across'))}">04 Across families</a></nav>${parametersHTML(f.Parameters)}${f.Session?.Conversation ? `<p class="references">Conversation arrives at ${link(route(name, 'event', 'server', f.Session.Conversation.Event), f.Session.Conversation.Event)} · ${esc(f.Session.Conversation.Path)}</p>` : ''}<section id="${esc(anchor(name, 'section', 'board'))}"><div class="section-heading"><span class="section-no">01</span><h2>The exchange board</h2><span class="section-note">Open a row to see its anatomy</span></div><div class="board-head"><span class="client">CLIENT</span><span>ON THE WIRE</span><span class="server">SERVER</span></div>${board || '<p class="empty">This family declares no exchanges.</p>'}</section><section id="${esc(anchor(name, 'section', 'vocabulary'))}"><div class="section-heading"><span class="section-no">02</span><h2>A shared vocabulary</h2><span class="section-note">Open a type to explore its shape</span></div>${vocabulary || '<p class="empty">No types declared.</p>'}</section><section id="${esc(anchor(name, 'section', 'errors'))}"><div class="section-heading"><span class="section-no">03</span><h2>When an exchange fails</h2></div>${errors || '<p class="empty">No public errors declared.</p>'}</section><section id="${esc(anchor(name, 'section', 'across'))}"><div class="section-heading"><span class="section-no">04</span><h2>Across families</h2></div>${shared ? `<table class="comparison"><thead><tr><th>Shared name</th><th>Declared in</th><th>Wire shape</th></tr></thead><tbody>${shared}</tbody></table>` : '<p class="empty">No type name is shared by multiple families.</p>'}</section>`;
}

export function typeHTML(atlas, family, name, lens = 'wire') {
  const t = lookup(atlas, family, name);
  if (!t) return '<h2 id="drawer-title">Type not found</h2>';
  const language = t.Languages?.[lens];
  const metadata = [
    t.Inline ? 'Derived from an inline shape.' : '',
    t.Open ? 'Open record.' : '',
    t.Key ? `Identity: ${t.Key}.` : '',
    t.Alias ? `Alias of ${typeText(t.Alias)}.` : '',
    list(t.Extends).length ? `Extends ${t.Extends.map(typeText).join(', ')}.` : '',
    list(t.Uses).length
      ? `Uses ${t.Uses.map((u) => (u.Type ? `${u.Parameter}.${u.Type}` : u.Parameter)).join(', ')}.`
      : '',
  ]
    .filter(Boolean)
    .join(' ');
  return `<span class="eyebrow">${esc(family)} / ${esc(t.Kind)}</span><h2 id="drawer-title">${esc(language?.Name || t.Name)}</h2><p class="description">${esc(t.Description)}</p><div class="type-meta">${esc(metadata)}</div>${parametersHTML(t.Parameters)}${code(json(t.Example))}<div class="annotation">${annotationHTML(annotate(atlas, family, name, t.Example))}</div>${list(t.Values).length ? `<p>Values: ${esc(t.Values.map((v) => JSON.stringify(v)).join(' · '))}</p>` : ''}${list(t.UsedBy).length ? `<h3 class="declaration">Used by</h3><div class="references">${t.UsedBy.map((r) => link(refRoute(family, r), `${r.Name} · ${r.At}`)).join('')}</div>` : ''}${language?.Declare ? `<h3 class="declaration">Declaration · ${esc(lens)}</h3>${code(language.Declare)}` : ''}`;
}
