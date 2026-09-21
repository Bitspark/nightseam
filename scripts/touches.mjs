// What an issue's fields claim, and the notes a pull request gets for not
// keeping them: **Touches**, the directories it says it edits, and
// **Changelog**, the entry it says it will land.
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
//
// Both notes report and never refuse, for the same reason: this repository
// refuses a derivation — a link, the README's matrix table, a driver op with
// no row — and notes a judgment. Whether a change needs recording is a
// judgment, which is why the issue makes it and this only compares the answer
// to what the pull request did.

/** The backticked paths an issue's Touches names, in either shape, or none. */
export function named(body) {
  const text = section(body ?? "") ?? "";
  return [...text.matchAll(/`([^`]+)`/g)].map(match => match[1].replace(/\/?\*\*$/, "").replace(/\/$/, ""));
}

/**
 * What an issue's **Changelog** field says it will land: `"entry"` when it
 * spells one, `"none"` when it says there is nothing to record, and
 * `"absent"` when the issue has no such field.
 *
 * `absent` is its own answer and is silent, which is the one place this
 * deliberately differs from Touches. Touches notes its own absence, because
 * every lane edits something and an issue naming nothing was checked against
 * nothing (#211). Every issue in the tree predates the Changelog field, so a
 * note on its absence would fire on all of them at once and be turned off the
 * day it landed. The field earns its silence by being asked for at filing.
 */
export function declared(body) {
  const text = section(body ?? "", "Changelog");
  if (text === null) return "absent";
  const said = text.trim().replace(/^[-*]\s+/, "");
  if (!said) return "absent";
  return /^none\b/i.test(said) ? "none" : "entry";
}

/**
 * The text of a field: the rest of the line after a bold inline label, or
 * everything under a heading of that name up to the next heading. `null` when
 * the body has no such field at all, which a caller may need to tell apart
 * from a field left empty.
 */
function section(body, label = "Touches") {
  const inline = body.match(new RegExp(`\\*\\*${label}:?\\*\\*[ \\t]*([^\\n]*)`, "i"));
  if (inline) return inline[1];
  const lines = body.split(/\r?\n/);
  const at = lines.findIndex(line => new RegExp(`^#{1,6}[ \\t]*${label}:?[ \\t]*$`, "i").test(line));
  if (at < 0) return null;
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
  // A lane that declared a changelog entry declared `CHANGELOG.md` with it:
  // the entry has to land somewhere. Noting every such lane for the one file
  // its own Changelog field promised would be noise this check itself created,
  // which is what dogfooding #378 on its own pull request showed. The note
  // still names only the paths the issue wrote, never this implied one.
  const scope = declared(issue.body) === "entry" ? [...paths, "CHANGELOG.md"] : paths;
  const outside = areas.filter(area => !scope.some(path => area.startsWith(path) || path.startsWith(area)));
  if (!outside.length) return "";
  return `Scope note (not a refusal): this PR changes ${tick(outside)}, which #${issue.number}'s **Touches** does not name (${tick(paths)}). If that is right, fine — say so in a line here so the next reader knows it was meant; if it is another lane's file, take it out.`;
}

/**
 * The note a pull request gets for declaring an entry and landing none, or ""
 * for none. Never a refusal: the lane may have found there was nothing to say
 * after all, and saying so in a line here is cheaper than editing the issue.
 *
 * It asks only what the issue answered. There is no list of paths that do or
 * do not deserve an entry: such a list rots, and "changed the tree without
 * touching CHANGELOG.md" is a proxy for a judgment that fails both ways — it
 * fires on a lane that rightly has nothing to record, and it passes #325,
 * which shipped inside v0.5.0 having extended `scripts/docs.mjs` without
 * extending the sentence in the changelog that described it.
 */
export function changelogNote(issue, files) {
  if (declared(issue.body) !== "entry") return "";
  if (files.some(file => file === "CHANGELOG.md")) return "";
  return `Changelog note (not a refusal): #${issue.number} declares a **Changelog** entry and this PR does not touch \`CHANGELOG.md\`. Add it under *Unreleased* so the release reads what landed from the lane that landed it; if the lane found there was nothing to record, say so in a line here.`;
}
