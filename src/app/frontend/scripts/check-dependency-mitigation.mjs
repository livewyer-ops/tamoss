import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "vite";

const chain = [
  "@byomakase/omakase-player",
  "patch-package",
  "find-yarn-workspace-root",
  "micromatch",
  "braces",
];

export async function checkDependencyMitigation(root) {
  const readJson = (path) =>
    JSON.parse(readFileSync(resolve(root, path), "utf8"));
  const ignoreScripts = execFileSync(
    "npm",
    ["config", "get", "ignore-scripts"],
    {
      cwd: root,
      encoding: "utf8",
    },
  ).trim();
  assert.equal(
    ignoreScripts,
    "true",
    "Dependency lifecycle scripts must remain disabled",
  );

  const lock = readJson("package-lock.json");
  const names = new Set(chain);
  for (const [location, entry] of Object.entries(lock.packages)) {
    const name = entry.name ?? location.split("node_modules/").at(-1);
    if (names.has(name)) {
      assert.equal(
        location,
        `node_modules/${name}`,
        `${name} package location changed`,
      );
    }
  }
  const expectedEdges = chain.map(
    (name, index) =>
      `${index === 0 ? "" : `node_modules/${chain[index - 1]}`}:dependencies:${name}`,
  );
  const edges = [];
  for (const [location, entry] of Object.entries(lock.packages)) {
    for (const field of [
      "dependencies",
      "devDependencies",
      "optionalDependencies",
      "peerDependencies",
    ]) {
      for (const name of Object.keys(entry[field] ?? {})) {
        if (names.has(name)) edges.push(`${location}:${field}:${name}`);
      }
    }
  }
  assert.deepEqual(
    edges.sort(),
    expectedEdges.sort(),
    "Install-only dependency consumers changed",
  );

  const restricted = chain.slice(1);
  let inspectedModules = 0;
  const inspect = () => ({
    name: "check-dependency-mitigation",
    generateBundle() {
      for (const id of this.getModuleIds()) {
        const normalised = id.replaceAll("\\", "/");
        assert(
          !restricted.some(
            (name) =>
              normalised === name ||
              normalised.startsWith(`${name}/`) ||
              normalised.includes(`/node_modules/${name}/`),
          ),
          `Install-only dependency entered the build: ${id}`,
        );
        inspectedModules++;
      }
    },
  });
  await build({
    root,
    configLoader: "runner",
    logLevel: "error",
    build: { write: false },
    plugins: [inspect()],
    worker: { plugins: () => [inspect()] },
  });
  assert(inspectedModules > 0, "No production modules were inspected");
  console.log(
    `GHSA-vfj7-8cjw-p6xm mitigation verified across ${inspectedModules} build modules.`,
  );
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  await checkDependencyMitigation(
    resolve(dirname(fileURLToPath(import.meta.url)), ".."),
  );
}
