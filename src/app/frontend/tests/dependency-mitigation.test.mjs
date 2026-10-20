import {
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { checkDependencyMitigation } from "../scripts/check-dependency-mitigation.mjs";

const frontend = resolve(import.meta.dirname, "..");
let root;
let lock;

beforeEach(() => {
  root = mkdtempSync(resolve(tmpdir(), "tamoss-dependency-mitigation-"));
  lock = JSON.parse(
    readFileSync(resolve(frontend, "package-lock.json"), "utf8"),
  );
  symlinkSync(
    resolve(frontend, "node_modules"),
    resolve(root, "node_modules"),
    "dir",
  );
  writeFileSync(resolve(root, "package.json"), '{"type":"module"}');
  writeFileSync(resolve(root, "package-lock.json"), JSON.stringify(lock));
  writeFileSync(resolve(root, ".npmrc"), "ignore-scripts=true\n");
  writeFileSync(resolve(root, "vite.config.mjs"), "export default {};");
  writeFileSync(
    resolve(root, "index.html"),
    '<script type="module" src="/main.js"></script>',
  );
  writeFileSync(resolve(root, "main.js"), "console.log('static frontend');");
});

afterEach(() => {
  vi.unstubAllEnvs();
  rmSync(root, { recursive: true, force: true });
});

it("accepts the pinned dependencies when absent from production code", async () => {
  await expect(checkDependencyMitigation(root)).resolves.toBeUndefined();
});

it("rejects an environment override enabling lifecycle scripts", async () => {
  vi.stubEnv("npm_config_ignore_scripts", "false");
  await expect(checkDependencyMitigation(root)).rejects.toThrow(
    "lifecycle scripts must remain disabled",
  );
});

it("rejects an additional consumer even when the package version is unchanged", async () => {
  lock.packages[""].devDependencies.braces = "3.0.3";
  writeFileSync(resolve(root, "package-lock.json"), JSON.stringify(lock));
  await expect(checkDependencyMitigation(root)).rejects.toThrow(
    "dependency consumers changed",
  );
});

it.each([
  "node_modules/extra/node_modules/braces",
  "node_modules/renamed-braces",
])("rejects an additional package copy at %s", async (location) => {
  lock.packages[location] = {
    ...lock.packages["node_modules/braces"],
    name: "braces",
  };
  writeFileSync(resolve(root, "package-lock.json"), JSON.stringify(lock));
  await expect(checkDependencyMitigation(root)).rejects.toThrow(
    "package location changed",
  );
});

it.each(["patch-package", "find-yarn-workspace-root", "micromatch", "braces"])(
  "rejects %s externalised by the bundler",
  async (name) => {
    writeFileSync(
      resolve(root, "main.js"),
      `import dependency from "${name}"; console.log(dependency);`,
    );
    writeFileSync(
      resolve(root, "vite.config.mjs"),
      `export default {build: {rollupOptions: {external: ["${name}"]}}};`,
    );
    await expect(checkDependencyMitigation(root)).rejects.toThrow(
      "Install-only dependency entered the build",
    );
  },
);

it.each([
  'import braces from "braces"; console.log(braces("{a,b}"));',
  'window.expand = () => import("braces");',
  'new Worker(new URL("./worker.js", import.meta.url), {type: "module"});',
])("rejects a production import: %s", async (source) => {
  writeFileSync(resolve(root, "main.js"), source);
  writeFileSync(
    resolve(root, "worker.js"),
    'import braces from "braces"; console.log(braces("{a,b}"));',
  );
  await expect(checkDependencyMitigation(root)).rejects.toThrow(
    "Install-only dependency entered the build",
  );
});

it("fails when production code cannot be inspected", async () => {
  writeFileSync(
    resolve(root, "main.js"),
    'import missing from "missing-package"; console.log(missing);',
  );
  await expect(checkDependencyMitigation(root)).rejects.toThrow();
});
