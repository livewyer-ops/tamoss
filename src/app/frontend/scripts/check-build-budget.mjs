import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const INITIAL_JS_GZIP_LIMIT = 200 * 1024;
const DIRECT_RUNTIME_PACKAGE_LIMIT = 7;

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(
  readFileSync(resolve(frontendRoot, "dist/.vite/manifest.json"), "utf8"),
);
const packageManifest = JSON.parse(
  readFileSync(resolve(frontendRoot, "package.json"), "utf8"),
);

const entryKeys = Object.entries(manifest)
  .filter(([, chunk]) => chunk.isEntry)
  .map(([key]) => key);
if (entryKeys.length !== 1) {
  throw new Error(
    `Expected one application entry chunk, found ${entryKeys.length}`,
  );
}

const initialFiles = new Set();
function addStaticImports(key) {
  const chunk = manifest[key];
  if (!chunk) throw new Error(`Build manifest references missing chunk ${key}`);
  if (chunk.file.endsWith(".js")) initialFiles.add(chunk.file);
  for (const importedKey of chunk.imports ?? []) addStaticImports(importedKey);
}
addStaticImports(entryKeys[0]);

let initialGzipBytes = 0;
for (const file of initialFiles) {
  initialGzipBytes += gzipSync(
    readFileSync(resolve(frontendRoot, "dist", file)),
  ).byteLength;
}

const directRuntimePackages = Object.keys(
  packageManifest.dependencies ?? {},
).filter((name) => !name.startsWith("@byomakase/"));

console.log(
  `Frontend budget: ${formatKiB(initialGzipBytes)} KiB initial JS gzip across ${initialFiles.size} chunk(s); ${directRuntimePackages.length} direct shell runtime package(s).`,
);

if (initialGzipBytes > INITIAL_JS_GZIP_LIMIT) {
  throw new Error(
    `Initial JavaScript is ${formatKiB(initialGzipBytes)} KiB gzip; budget is ${formatKiB(INITIAL_JS_GZIP_LIMIT)} KiB.`,
  );
}
if (directRuntimePackages.length > DIRECT_RUNTIME_PACKAGE_LIMIT) {
  throw new Error(
    `Shell has ${directRuntimePackages.length} direct runtime packages; budget is ${DIRECT_RUNTIME_PACKAGE_LIMIT}.`,
  );
}

function formatKiB(bytes) {
  return (bytes / 1024).toFixed(2);
}
