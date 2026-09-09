import { execFileSync } from "node:child_process";
import { readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(frontendRoot, "../../..");
const openapiTypescript = resolve(
  frontendRoot,
  "node_modules/.bin/openapi-typescript",
);
const contracts = [
  {
    schemaPath: resolve(repoRoot, "src/openapi.yaml"),
    outPath: resolve(frontendRoot, "src/api/generated/openapi.ts"),
  },
  {
    schemaPath: resolve(repoRoot, "operator/internal/consoleapi/openapi.yaml"),
    outPath: resolve(frontendRoot, "src/control/generated/openapi.ts"),
  },
];
const check = process.argv.includes("--check");

for (const contract of contracts) {
  const generatedPath = check
    ? resolve(tmpdir(), `tamoss-${process.pid}-${basename(contract.outPath)}`)
    : contract.outPath;
  try {
    execFileSync(
      openapiTypescript,
      [contract.schemaPath, "-o", generatedPath],
      {
        cwd: frontendRoot,
        stdio: "inherit",
      },
    );
    if (
      check &&
      readFileSync(generatedPath, "utf8") !==
        readFileSync(contract.outPath, "utf8")
    ) {
      throw new Error(`Generated OpenAPI types are stale: ${contract.outPath}`);
    }
  } finally {
    if (check) rmSync(generatedPath, { force: true });
  }
}
