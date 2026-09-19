// The conformance matrix as the README and the release read it:
// `conformance/matrix.json`, what `conformance/profiles.json`'s tier table
// says of each row, and the table a consumer sees. Nothing here runs the
// suite — `conformance/go` writes the file, from `TestMain`, after a run —
// so a table is only ever as fresh as the run that wrote it, which is what
// the check in CI is for. docs/languages/tiers.md says what a profile and a tier mean.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { root } from "./packages.mjs";

export const matrixFile = join(root, "conformance/matrix.json");
export const profilesFile = join(root, "conformance/profiles.json");

export const readJSON = file => JSON.parse(readFileSync(file, "utf8"));

/**
 * The profile columns, in the order `profiles.json` declares them, which is
 * the order a language is built in — core, generator, tunnel,
 * observability — rather than the alphabetical order the matrix lists them
 * in. A profile the matrix knows and the declaration does not comes after,
 * so that a column is never silently dropped.
 */
export function columns(matrix, profiles) {
  const declared = Object.keys(profiles.profiles ?? {});
  const known = matrix.profiles ?? [];
  return [...declared.filter(name => known.includes(name)), ...known.filter(name => !declared.includes(name))];
}

/**
 * Every language `profiles.json` places at a tier that the matrix has no row
 * for. A run filtered by `-run` writes a file with a language missing, and a
 * table rendered from one would quietly lose a row rather than say so.
 */
export function missing(matrix, profiles) {
  const rows = matrix.languages ?? {};
  return Object.keys(profiles.languages ?? {}).filter(language => !rows[language]).sort();
}

/**
 * Every profile a language's row has no cell for at all. The suite records a
 * cell for each profile it placed a scenario in, skips included, so a column
 * that is simply absent is a run that never reached it — which a table would
 * show as an em dash and a gate would read as nothing failing. Named per
 * language, since a language may enter the matrix before it carries a
 * profile's testee.
 */
export function unrun(matrix, profiles) {
  const declared = Object.keys(profiles.profiles ?? {});
  const out = [];
  for (const language of Object.keys(matrix.languages ?? {}).sort())
    for (const profile of declared)
      if (!matrix.languages[language].cells?.[profile]) out.push(`${language} has no ${profile} cell`);
  return out;
}

/** What one cell says: passed, what was skipped with it, what failed. */
export function cell(value) {
  if (!value) return "—";
  const { passed = 0, skipped = 0, failed = 0 } = value;
  if (failed > 0) return `✗ ${failed} failed`;
  if (passed === 0 && skipped === 0) return "—";
  if (passed === 0) return `— ${skipped} skipped`;
  return skipped > 0 ? `✓ ${skipped} skipped` : "✓";
}

/**
 * The Markdown table: a row per language with its tier, a column per profile,
 * and the verdict the suite recorded. The reference is marked, since every
 * other language is held to it and a reader should know which one it is.
 */
export function table(matrix, profiles) {
  const names = columns(matrix, profiles);
  const languages = Object.keys(matrix.languages ?? {}).sort();
  const lines = [
    `| language | tier | ${names.join(" | ")} | verdict |`,
    `| --- | --- | ${names.map(() => "---").join(" | ")} | --- |`,
  ];
  for (const language of languages) {
    const row = matrix.languages[language];
    const reference = profiles.languages?.[language]?.reference ? " *(reference)*" : "";
    const cells = names.map(name => cell(row.cells?.[name]));
    lines.push(`| \`${language}\`${reference} | ${row.tier ?? "—"} | ${cells.join(" | ")} | ${row.verdict ?? "—"} |`);
  }
  return lines.join("\n");
}

/**
 * The languages `profiles.json` plans and no testee carries yet, by tier, as
 * one sentence — so the table says what is coming as well as what is held,
 * and a reader is not left to infer that an absent language is a refused one.
 */
export function planned(profiles) {
  const entries = Object.entries(profiles.planned ?? {});
  if (entries.length === 0) return "";
  const byTier = new Map();
  for (const [language, tier] of entries) {
    if (!byTier.has(tier)) byTier.set(tier, []);
    byTier.get(tier).push(language);
  }
  const parts = [...byTier.entries()]
    .sort(([a], [b]) => a - b)
    .map(([tier, languages]) => `${languages.sort().map(l => `\`${l}\``).join(", ")} at tier ${tier}`);
  return `Planned, with no testee yet: ${parts.join("; ")}.`;
}

/**
 * What the tier table says of a matrix at release time, per docs/languages/tiers.md:
 * a failure in a profile the tier *requires* is what its `onFailure` says —
 * `stop` refuses the tag, `provisional` ships and marks the language in the
 * notes — and a failure elsewhere is what its `otherwise` says, `stop-next`
 * meaning this release ships and the next one refuses if it is still
 * failing. The lag is read against `previous`, the matrix of the last
 * release; with none, nothing is a second failure and the first one ships.
 *
 * The verdict recorded in `matrix.json` is the required-profile outcome
 * alone, which is what the suite gates on; the lag lives between two
 * releases and so is computed here, from the cells, rather than read off a
 * row that cannot know what the last release held.
 */
export function gate(matrix, profiles, previous) {
  const problems = [];
  const provisional = [];
  const lagging = [];
  for (const language of Object.keys(matrix.languages ?? {}).sort()) {
    const row = matrix.languages[language];
    const tier = profiles.tiers?.[String(row.tier)];
    // A language with no tier is in the matrix and promises nothing yet;
    // every cell of its row is informational.
    if (!tier) continue;
    const required = tier.requires ?? [];
    const failed = Object.entries(row.cells ?? {})
      .filter(([, value]) => (value?.failed ?? 0) > 0)
      .map(([name]) => name)
      .sort();
    const inRequired = failed.filter(name => required.includes(name));
    const elsewhere = failed.filter(name => !required.includes(name));
    if (inRequired.length > 0) {
      const what = `\`${language}\` (tier ${row.tier}) fails ${inRequired.join(", ")}`;
      if (tier.onFailure === "stop") problems.push(`${what}, which tier ${row.tier} stops a release for`);
      else if (tier.onFailure === "provisional") provisional.push(`${what}; tier ${row.tier} ships provisional`);
    }
    if (elsewhere.length > 0 && tier.otherwise === "stop-next") {
      const before = wasLagging(previous, language, profiles);
      const what = `\`${language}\` (tier ${row.tier}) fails ${elsewhere.join(", ")}`;
      if (before) problems.push(`${what}, and did at the last release; tier ${row.tier} allows one release of lag and this is the second`);
      else lagging.push(`${what}; tier ${row.tier} allows one release of lag, so the next release refuses it`);
    }
  }
  return { problems, provisional, lagging };
}

/** Whether the last release's matrix had this language failing outside what its tier requires. */
function wasLagging(previous, language, profiles) {
  const row = previous?.languages?.[language];
  if (!row) return false;
  const required = profiles.tiers?.[String(row.tier)]?.requires ?? [];
  return Object.entries(row.cells ?? {}).some(([name, value]) => (value?.failed ?? 0) > 0 && !required.includes(name));
}
