// Data alone, in TypeScript: a model-tier family encodes and validates with
// no peer, no connection, no tunnel and no scope anywhere in the file. It is
// the same independence check the Go case makes, and it runs on its own —
// nothing starts a server for it, because nothing needs one.

import assert from 'node:assert/strict';
import { validateWire, type Notebook } from './api/ts/notes-client/src/index.ts';

const notebook: Notebook = {
  title: 'field',
  notes: [{ id: 'first', body: 'one', tags: ['a'], revision: 1 }],
  by_id: { first: { id: 'first', body: 'one', tags: ['a'], revision: 1 } },
  latest: { id: 'first', body: 'one', tags: ['a'], revision: 1 },
};

validateWire('Notebook', notebook);
const read = JSON.parse(JSON.stringify(notebook)) as Notebook;
validateWire('Notebook', read);
assert.deepEqual(read, notebook);

// The declaration's own constraints, checked by the family's validator with
// nothing to check them over.
assert.throws(() => validateWire('Note', { id: 'Not Lower', body: 'x', tags: [], revision: 0 }));
assert.throws(() => validateWire('Note', { id: 'ok', body: 'x', tags: [], revision: -1 }));
assert.throws(() => validateWire('Note', { id: 'ok', body: 'x', tags: [] }));

// A nullable member is null, and an absent one is absent: two facts, kept
// apart, with no live anything involved in either.
validateWire('Notebook', { ...notebook, latest: null });
const { latest: _omitted, ...withoutLatest } = notebook;
validateWire('Notebook', withoutLatest);

console.log('data alone: ok');
