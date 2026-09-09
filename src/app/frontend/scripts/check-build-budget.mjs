import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const INITIAL_JS_GZIP_LIMIT = 200 * 1024;
const OMAKASE_LAZY_GZIP_LIMIT = 1.4 * 1024 * 1024;
const DIRECT_RUNTIME_PACKAGE_LIMIT = 7;
const OMAKASE_PACKAGE = "@byomakase/omakase-player";
const OMAKASE_VERSION = "1.1.1";
const OMAKASE_INTEGRITY =
  "sha512-Bc5Md7N3hpeSBeTJgjg1/qNeUmm2MNmSv2cgxmrOoTzXYjoySjczlfRZQG0Rwyz+qarYTcwMCqt9yvLOhGapHA==";
const HLS_PACKAGE = "hls.js";
const HLS_VERSION = "1.6.17";
const HLS_INTEGRITY =
  "sha512-NUplVGVuc1hSPwdB/9/cbRkUmLrYi75/hqiXKdA+l300pJNxDu96R7jRb2imDzWJqIUF4I5ThmAdp9GvOCXsuQ==";
const OMAKASE_PREVIEW_SOURCE = "src/player/MediaPreview.tsx";
const OMAKASE_JS_MARKERS = ["OmakaseTrackApi", "OmakaseVttVersion"];
const OMAKASE_CSS_MARKER = "omakase-player";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(
  readFileSync(resolve(frontendRoot, "dist/.vite/manifest.json"), "utf8"),
);
const packageManifest = JSON.parse(
  readFileSync(resolve(frontendRoot, "package.json"), "utf8"),
);
const packageLock = JSON.parse(
  readFileSync(resolve(frontendRoot, "package-lock.json"), "utf8"),
);

assertExactOmakasePackage();
assertExactHlsPackage();

const entryKeys = Object.entries(manifest)
  .filter(([, chunk]) => chunk.isEntry)
  .map(([key]) => key);
if (entryKeys.length !== 1) {
  throw new Error(
    `Expected one application entry chunk, found ${entryKeys.length}`,
  );
}

const entryKey = entryKeys[0];
const initialKeys = collectReachableKeys(entryKey, ["imports"]);
const initialFiles = collectFiles(initialKeys);
const previewKey = findSourceKey(OMAKASE_PREVIEW_SOURCE);
const previewChunk = manifest[previewKey];
if (!previewChunk.isDynamicEntry) {
  throw new Error(`${OMAKASE_PREVIEW_SOURCE} must remain a dynamic entry.`);
}
if (initialKeys.has(previewKey)) {
  throw new Error(
    "Omakase preview is statically reachable from the application entry.",
  );
}
const allApplicationKeys = collectReachableKeys(entryKey, [
  "imports",
  "dynamicImports",
]);
if (!allApplicationKeys.has(previewKey)) {
  throw new Error(
    "Omakase preview is not dynamically reachable from the application entry.",
  );
}

const previewKeys = collectLazyReachableKeys(previewKey, initialKeys);
const previewFiles = collectFiles(previewKeys);
const lazyPreviewFiles = new Set(
  [...previewFiles].filter((file) => !initialFiles.has(file)),
);
assertOmakaseAssetIsolation(initialFiles, lazyPreviewFiles);

let initialGzipBytes = 0;
for (const file of [...initialFiles].filter((name) => name.endsWith(".js"))) {
  initialGzipBytes += gzipSync(
    readFileSync(resolve(frontendRoot, "dist", file)),
  ).byteLength;
}

let omakaseLazyGzipBytes = 0;
for (const file of lazyPreviewFiles) {
  omakaseLazyGzipBytes += gzipSync(
    readFileSync(resolve(frontendRoot, "dist", file)),
  ).byteLength;
}

const directRuntimePackages = Object.keys(
  packageManifest.dependencies ?? {},
).filter((name) => !name.startsWith("@byomakase/"));

console.log(
  `Frontend budget: ${formatKiB(initialGzipBytes)} KiB initial JS gzip; ${formatKiB(omakaseLazyGzipBytes)} KiB lazy Omakase preview JS/CSS gzip across ${lazyPreviewFiles.size} file(s); ${directRuntimePackages.length} direct runtime package(s) excluding @byomakase packages.`,
);

if (initialGzipBytes > INITIAL_JS_GZIP_LIMIT) {
  throw new Error(
    `Initial JavaScript is ${formatKiB(initialGzipBytes)} KiB gzip; budget is ${formatKiB(INITIAL_JS_GZIP_LIMIT)} KiB.`,
  );
}
if (directRuntimePackages.length > DIRECT_RUNTIME_PACKAGE_LIMIT) {
  throw new Error(
    `The UI has ${directRuntimePackages.length} direct runtime packages excluding @byomakase packages; budget is ${DIRECT_RUNTIME_PACKAGE_LIMIT}.`,
  );
}
if (omakaseLazyGzipBytes > OMAKASE_LAZY_GZIP_LIMIT) {
  throw new Error(
    `Lazy Omakase preview is ${formatKiB(omakaseLazyGzipBytes)} KiB gzip; budget is ${formatKiB(OMAKASE_LAZY_GZIP_LIMIT)} KiB.`,
  );
}

function assertExactOmakasePackage() {
  const directOmakasePackages = Object.keys(
    packageManifest.dependencies ?? {},
  ).filter((name) => name.startsWith("@byomakase/"));
  if (
    directOmakasePackages.length !== 1 ||
    directOmakasePackages[0] !== OMAKASE_PACKAGE
  ) {
    throw new Error(
      `Expected ${OMAKASE_PACKAGE} to be the only direct @byomakase dependency.`,
    );
  }
  if (packageManifest.dependencies[OMAKASE_PACKAGE] !== OMAKASE_VERSION) {
    throw new Error(
      `${OMAKASE_PACKAGE} must be exactly pinned to ${OMAKASE_VERSION}.`,
    );
  }

  const lockedRoot = packageLock.packages?.[""];
  const lockedPlayer =
    packageLock.packages?.[`node_modules/${OMAKASE_PACKAGE}`];
  if (lockedRoot?.dependencies?.[OMAKASE_PACKAGE] !== OMAKASE_VERSION) {
    throw new Error(
      `package-lock.json does not preserve the exact Omakase pin.`,
    );
  }
  if (
    lockedPlayer?.version !== OMAKASE_VERSION ||
    lockedPlayer?.integrity !== OMAKASE_INTEGRITY
  ) {
    throw new Error(
      `package-lock.json does not contain the reviewed Omakase ${OMAKASE_VERSION} artifact.`,
    );
  }
}

function assertExactHlsPackage() {
  const lockedRoot = packageLock.packages?.[""];
  const lockedHls = packageLock.packages?.[`node_modules/${HLS_PACKAGE}`];
  if (
    packageManifest.dependencies?.[HLS_PACKAGE] !== HLS_VERSION ||
    lockedRoot?.dependencies?.[HLS_PACKAGE] !== HLS_VERSION
  ) {
    throw new Error(`${HLS_PACKAGE} must be exactly pinned to ${HLS_VERSION}.`);
  }
  if (
    lockedHls?.version !== HLS_VERSION ||
    lockedHls?.integrity !== HLS_INTEGRITY
  ) {
    throw new Error(
      `package-lock.json does not contain the reviewed ${HLS_PACKAGE} ${HLS_VERSION} artifact.`,
    );
  }
}

function findSourceKey(source) {
  const matches = Object.entries(manifest)
    .filter(([key, chunk]) => key === source || chunk.src === source)
    .map(([key]) => key);
  if (matches.length !== 1) {
    throw new Error(
      `Expected one build manifest entry for ${source}, found ${matches.length}.`,
    );
  }
  return matches[0];
}

function collectReachableKeys(startKey, relationshipFields) {
  const visited = new Set();
  const pending = [startKey];
  while (pending.length) {
    const key = pending.pop();
    if (visited.has(key)) continue;
    const chunk = manifest[key];
    if (!chunk) {
      throw new Error(`Build manifest references missing chunk ${key}`);
    }
    visited.add(key);
    for (const field of relationshipFields) {
      pending.push(...(chunk[field] ?? []));
    }
  }
  return visited;
}

function collectLazyReachableKeys(startKey, initialStaticKeys) {
  const visited = new Set();
  const pending = [startKey];
  while (pending.length) {
    const key = pending.pop();
    if (visited.has(key)) continue;
    const chunk = manifest[key];
    if (!chunk) {
      throw new Error(`Build manifest references missing chunk ${key}`);
    }
    visited.add(key);
    pending.push(...(chunk.imports ?? []));
    if (!initialStaticKeys.has(key)) {
      pending.push(...(chunk.dynamicImports ?? []));
    }
  }
  return visited;
}

function collectFiles(keys) {
  const files = new Set();
  for (const key of keys) {
    const chunk = manifest[key];
    if (chunk.file.endsWith(".js") || chunk.file.endsWith(".css")) {
      files.add(chunk.file);
    }
    for (const cssFile of chunk.css ?? []) files.add(cssFile);
    for (const assetFile of chunk.assets ?? []) {
      if (assetFile.endsWith(".js") || assetFile.endsWith(".css")) {
        files.add(assetFile);
      }
    }
  }
  return files;
}

function assertOmakaseAssetIsolation(initialAssetFiles, lazyAssetFiles) {
  for (const file of initialAssetFiles) {
    const content = readFileSync(resolve(frontendRoot, "dist", file), "utf8");
    const containsOmakase = file.endsWith(".css")
      ? content.includes(OMAKASE_CSS_MARKER)
      : OMAKASE_JS_MARKERS.some((marker) => content.includes(marker));
    if (containsOmakase) {
      throw new Error(`Omakase code leaked into initial asset ${file}.`);
    }
  }

  const lazyJavaScript = [...lazyAssetFiles]
    .filter((file) => file.endsWith(".js"))
    .some((file) =>
      OMAKASE_JS_MARKERS.some((marker) =>
        readFileSync(resolve(frontendRoot, "dist", file), "utf8").includes(
          marker,
        ),
      ),
    );
  const lazyStyles = [...lazyAssetFiles]
    .filter((file) => file.endsWith(".css"))
    .some((file) =>
      readFileSync(resolve(frontendRoot, "dist", file), "utf8").includes(
        OMAKASE_CSS_MARKER,
      ),
    );
  if (!lazyJavaScript || !lazyStyles) {
    throw new Error(
      "Reviewed Omakase JavaScript and CSS were not both found in the lazy preview graph.",
    );
  }
}

function formatKiB(bytes) {
  return (bytes / 1024).toFixed(2);
}
