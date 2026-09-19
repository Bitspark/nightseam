// Renders the *Languages* table of README.md from conformance/matrix.json,
// between the two markers that fence it, and holds the checked-in table to
// the matrix with --check — the way `nightseam check` holds generated code,
// and for the same reason: the table is output, a reader takes it for the
// truth, and a stale one is worse than none. The suite writes the matrix;
// this only renders it.
//
//	node scripts/matrix-table.mjs            # rewrite the section
//	node scripts/matrix-table.mjs --check    # fail if it is stale
//
// --matrix and --readme name other files, which is how the tests drive it.
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { matrixFile, missing, planned, profilesFile, readJSON, table } from "./matrix.mjs";
import { root } from "./packages.mjs";

export const start = "<!-- matrix:start -->";
export const end = "<!-- matrix:end -->";

/** The section's body: the table, and what is planned beneath it. */
export function section(matrix, profiles) {
  const lines = [table(matrix, profiles)];
  const rest = planned(profiles);
  if (rest) lines.push("", rest);
  return lines.join("\n");
}

/** The document with the fenced section replaced. The markers must be there: this writes a section, it does not invent one. */
export function replace(document, body) {
  const from = document.indexOf(start);
  const to = document.indexOf(end);
  if (from < 0 || to < 0 || to < from) throw new Error(`no ${start} … ${end} to write the table between`);
  return document.slice(0, from + start.length) + "\n" + body + "\n" + document.slice(to);
}

function flag(name) {
  const at = process.argv.indexOf(name);
  return at < 0 ? undefined : process.argv[at + 1];
}

// Run as a script; imported by the tests, which call the functions above.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const check = process.argv.includes("--check");
  const readme = flag("--readme") ?? join(root, "README.md");
  const matrix = readJSON(flag("--matrix") ?? matrixFile);
  const profiles = readJSON(profilesFile);
  // A matrix written by a filtered run is missing a language, and a table
  // rendered from it would lose a row without saying so. Refuse it here
  // rather than write a README that is quietly short of a language.
  const absent = missing(matrix, profiles);
  if (absent.length > 0) {
    console.error(
      `conformance/matrix.json has no row for ${absent.join(", ")}, which profiles.json places at a tier.\n` +
        "It is the artifact of a whole run; a run filtered by -run writes a partial one.\n" +
        "Run the suite and try again: go test ./conformance/go -run 'TestSelf|TestStar|TestGenerated' -count=1",
    );
    process.exit(1);
  }
  const document = readFileSync(readme, "utf8");
  const written = replace(document, section(matrix, profiles));
  if (check) {
    if (written !== document) {
      console.error(
        "the Languages table in README.md is not what conformance/matrix.json holds.\n" +
          "Render it and commit the diff: node scripts/matrix-table.mjs",
      );
      process.exit(1);
    }
    console.log("README.md holds the matrix");
  } else if (written === document) {
    console.log("README.md already holds the matrix");
  } else {
    writeFileSync(readme, written);
    console.log("README.md: the Languages table rewritten from conformance/matrix.json");
  }
}
