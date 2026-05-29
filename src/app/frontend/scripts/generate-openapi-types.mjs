import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(frontendRoot, "../../..");
const outPath = resolve(frontendRoot, "src/api/generated/openapi.ts");
const schemaPath = resolve(repoRoot, "src/openapi.yaml");
const openapiTypescript = resolve(
  frontendRoot,
  "node_modules/.bin/openapi-typescript",
);

execFileSync(openapiTypescript, [schemaPath, "-o", outPath], {
  cwd: frontendRoot,
  stdio: "inherit",
});
