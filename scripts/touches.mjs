// What an issue's **Touches** names, and the scope note a pull request gets
// for changing something outside it.
//
// It is a module of its own so that a test can drive it: scripts/pr-scope.mjs
// reads a live pull request from `gh` the moment it is imported, and the part
// worth holding is this one — what the issue's text means — not the fetching.
//
// Two shapes, because both are in the tree. `.github/ISSUE_TEMPLATE/lane.yml`
// renders a `### Touches` heading with the paths beneath it, and hand-written
// issues use a bold inline `**Touches:**`. The parser knowing only the second
// is what #211 was: the note was a no-op for every form-filed lane, and said
// nothing about being one.

/** The backticked paths an issue's Touches names, in either shape, or none. */
export function named(body) {
  const text = section(body ?? "");
  return [...text.matchAll(/`([^`]+)`/g)].map(match => match[1].replace(/\/?\*\*$/, "").replace(/\/$/, ""));
}

/**
 * The text of the Touches field: the rest of the line after a bold inline
 * label, or everything under a `Touches` heading up to the next heading.
 */
function section(body) {
  const inline = body.match(/\*\*Touches:?\*\*[ \t]*([^\n]*)/i);
  if (inline) return inline[1];
  const lines = body.split(/\r?\n/);
  const at = lines.findIndex(line => /^#{1,6}[ \t]*Touches:?[ \t]*$/i.test(line));
  if (at < 0) return "";
  const rest = lines.slice(at + 1);
  const next = rest.findIndex(line => /^#{1,6}[ \t]/.test(line));
  return (next < 0 ? rest : rest.slice(0, next)).join("\n");
}

/**
 * The note a pull request gets, or "" for none. Never a refusal: the issue may
 * have guessed the files wrong, and a lane that knows better should not have
 * to edit the issue to land.
 *
 * An issue naming no Touches at all gets a note of its own. A check that holds
 * nothing and says nothing is indistinguishable from one that passed, which is
 * the whole of what went wrong here.
 */
export function scopeNote(issue, areas) {
  const paths = named(issue.body);
  const tick = list => list.map(one => "`" + one + "`").join(", ");
  if (!paths.length) {
    return `Scope note (not a refusal): #${issue.number} names no **Touches**, so the ${areas.length} ${areas.length === 1 ? "area" : "areas"} this PR changes (${tick(areas)}) were held against nothing. Add the line to the issue so the next lane through these files can be.`;
  }
  const outside = areas.filter(area => !paths.some(path => area.startsWith(path) || path.startsWith(area)));
  if (!outside.length) return "";
  return `Scope note (not a refusal): this PR changes ${tick(outside)}, which #${issue.number}'s **Touches** does not name (${tick(paths)}). If that is right, fine — say so in a line here so the next reader knows it was meant; if it is another lane's file, take it out.`;
}
