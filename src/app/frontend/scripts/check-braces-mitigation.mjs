import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "vite";

// Reassess GHSA-vfj7-8cjw-p6xm whenever this reviewed install-only chain changes.
const reviewed = [
  [
    "@byomakase/omakase-player",
    "1.1.1",
    "1.1.1",
    "sha512-Bc5Md7N3hpeSBeTJgjg1/qNeUmm2MNmSv2cgxmrOoTzXYjoySjczlfRZQG0Rwyz+qarYTcwMCqt9yvLOhGapHA==",
  ],
  [
    "patch-package",
    "8.0.1",
    "^8.0.1",
    "sha512-VsKRIA8f5uqHQ7NGhwIna6Bx6D9s/1iXlA1hthBVBEbkq+t4kXD0HHt+rJhf/Z+Ci0F/HCB2hvn0qLdLG+Qxlw==",
  ],
  [
    "find-yarn-workspace-root",
    "2.0.0",
    "^2.0.0",
    "sha512-1IMnbjt4KzsQfnhnzNd8wUEgXZ44IzZaZmnLYx7D5FZlaHt2gW20Cri8Q+E/t5tIj4+epTBub+2Zxu/vNILzqQ==",
  ],
  [
    "micromatch",
    "4.0.8",
    "^4.0.2",
    "sha512-PXwfBhYu0hBCPw8Dn0E+WDYb7af3dSLVWKi3HGv84IdF4TyFoC0ysxFd0Goxw7nSv4T/PzEJQxsYsEiFCKo2BA==",
  ],
  [
    "braces",
    "3.0.3",
    "^3.0.3",
    "sha512-yQbXgO/OSZVD2IsiLlro+7Hf6Q18EJrKSEsdoMzKePKXct3gvD8oLcOQdIzGupr5Fj+EDe8gO/lxc1BzfMpxvA==",
  ],
];

export async function checkBracesMitigation(root) {
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
  const names = new Set(reviewed.map(([name]) => name));
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
  for (const [name, version, range, integrity] of reviewed) {
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
    "Reviewed dependency consumers changed",
  );

  const restricted = reviewed.slice(1).map(([name]) => name);
  let inspectedModules = 0;
  const inspect = () => ({
    name: "check-braces-mitigation",
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
  await checkBracesMitigation(
    resolve(dirname(fileURLToPath(import.meta.url)), ".."),
  );
}
