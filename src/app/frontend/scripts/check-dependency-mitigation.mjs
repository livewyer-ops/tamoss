import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "vite";

import mitigation from "./dependency-mitigation.json" with { type: "json" };

const { advisory, chain } = mitigation;
assert(
  Array.isArray(chain) && chain.length > 1,
  "Dependency chain is required",
);

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
  const installed = readJson("node_modules/.package-lock.json");
  const names = new Set(chain.map(([name]) => name));
  for (const packages of [lock.packages, installed.packages]) {
    for (const [location, entry] of Object.entries(packages)) {
      const name = entry.name ?? location.split("node_modules/").at(-1);
      if (names.has(name)) {
        assert.equal(
          location,
          `node_modules/${name}`,
          `${name} package location changed`,
        );
      }
    }
  }
  const expectedEdges = [];
  let parent = "";
  for (const [name, version, range, integrity] of chain) {
    const location = `node_modules/${name}`;
    for (const packages of [lock.packages, installed.packages]) {
      assert.equal(
        packages[location]?.version,
        version,
        `${name} version changed`,
      );
      assert.equal(
        packages[location]?.integrity,
        integrity,
        `${name} integrity changed`,
      );
    }
    assert.equal(
      readJson(`${location}/package.json`).version,
      version,
      `${name} installation changed`,
    );
    expectedEdges.push(`${parent}:dependencies:${name}:${range}`);
    parent = location;
  }
  const edges = [];
  for (const [location, entry] of Object.entries(lock.packages)) {
    for (const field of [
      "dependencies",
      "devDependencies",
      "optionalDependencies",
      "peerDependencies",
    ]) {
      for (const [name, range] of Object.entries(entry[field] ?? {})) {
        if (names.has(name))
          edges.push(`${location}:${field}:${name}:${range}`);
      }
    }
  }
  assert.deepEqual(
    edges.sort(),
    expectedEdges.sort(),
    "Pinned dependency consumers changed",
  );

  const restricted = chain.slice(1).map(([name]) => name);
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
    `${advisory} mitigation verified across ${inspectedModules} build modules.`,
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
