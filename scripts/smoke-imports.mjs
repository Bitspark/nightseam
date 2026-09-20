// Both installation smokes ask the same question of every published entry
// point: can an outsider compile against its declarations and load it?
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { packages, publishedEntryPoints, root } from "./packages.mjs";

const declaredPackages = () => packages.map(directory => JSON.parse(readFileSync(join(root, directory, "package.json"), "utf8")));

/** Add only missing release packages to the copied consumer, never the source example. */
export function addPublishedDependencies(consumer, version, manifests = declaredPackages()) {
  const file = join(consumer, "package.json");
  const manifest = JSON.parse(readFileSync(file, "utf8"));
  const dependencies = manifest.dependencies ?? {};
  for (const { name } of manifests) {
    if (dependencies[name] !== undefined && dependencies[name] !== version) {
      throw new Error(`${name}: the consumer requires ${dependencies[name]}, not release ${version}`);
    }
    dependencies[name] = version;
  }
  manifest.dependencies = dependencies;
  writeFileSync(file, JSON.stringify(manifest, null, 2) + "\n");
}

/**
 * Check every declared public entry point, even for a package the example
 * never imports. The published map excludes workspace-only helpers and
 * includes subpaths without maintaining a second list in either smoke.
 * Library checking catches broken emitted declarations; Node catches
 * missing runtime dependencies and exports that only worked in the tree.
 */
export function holdOutsiderImports(consumer, { pnpm, run, step }, manifests = declaredPackages()) {
  const specifiers = manifests.flatMap(declared => publishedEntryPoints(declared).map(entry => declared.name + entry));
  const directory = join(consumer, "nightseam-imports");
  mkdirSync(directory, { recursive: true });
  const note = "// Written by scripts/smoke-imports.mjs into the copied consumer, never into the checkout.\n";
  writeFileSync(
    join(directory, "imports.ts"),
    note +
      specifiers.map((specifier, at) => `import * as module${at} from ${JSON.stringify(specifier)};`).join("\n") +
      `\n\nexport const imported: readonly unknown[] = [${specifiers.map((_, at) => `module${at}`).join(", ")}];\n`,
  );
  // The example may use skipLibCheck; the release's emitted declarations
  // must also compile when a consumer checks the libraries themselves.
  writeFileSync(
    join(directory, "tsconfig.json"),
    JSON.stringify(
      {
        compilerOptions: {
          target: "ES2022",
          module: "NodeNext",
          moduleResolution: "NodeNext",
          lib: ["ES2022", "DOM"],
          strict: true,
          skipLibCheck: false,
          types: ["node"],
          noEmit: true,
        },
        include: ["imports.ts"],
      },
      null,
      2,
    ) + "\n",
  );
  writeFileSync(
    join(directory, "load.mjs"),
    note +
      `const specifiers = ${JSON.stringify(specifiers, null, 2)};\n` +
      `for (const specifier of specifiers) {\n` +
      `  const loaded = await import(specifier);\n` +
      `  const exported = Object.keys(loaded).filter(name => name !== "default");\n` +
      `  if (exported.length === 0) throw new Error(specifier + " loaded and exported nothing; its installed entry point is empty");\n` +
      `  console.log("  loaded " + specifier + ", " + exported.length + " exports");\n` +
      `}\n`,
  );
  step(`tsc over an import of all ${specifiers.length} published entry points, with library checking on`);
  pnpm(["exec", "tsc", "-p", "nightseam-imports/tsconfig.json"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
  step("node, loading each of them from the installed packages");
  run("node", ["nightseam-imports/load.mjs"], { cwd: consumer, stdio: ["ignore", "inherit", "inherit"] });
}
