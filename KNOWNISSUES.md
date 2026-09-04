# Known Issues

## Dependency audit backlog

Reviewed 2026-09-04. `pip-audit`, production-scope `npm audit`, and npm
signature verification are clean. The entries below are conservative lockfile
findings reported by OSV and the development-scope npm audit.

The Go findings are transitive operator dependencies. Reachability is not
asserted because the repository audit deliberately disables Go call analysis
to stay reproducible across developer machines and CI runners. Upgrade them
through compatible Kubernetes/controller dependency releases, using the Go
Dependabot coverage added in #188, and keep them failing under `STRICT=1`.

The npm findings are confined to development and build tooling and are absent
from the production-scope npm audit. They still execute in CI, so npm lifecycle
scripts are disabled by #191 and each upstream toolchain fix remains required.

| Ecosystem | Package | Locked version | Advisory IDs |
| --- | --- | --- | --- |
| Go | `github.com/google/cel-go` | 0.26.0 | GO-2026-6094 |
| Go | `github.com/klauspost/compress` | 1.18.0 | GO-2026-5841 |
| Go | `go.opentelemetry.io/otel` | 1.43.0 | GO-2026-5158 |
| Go | `golang.org/x/crypto` | 0.50.0 | GO-2026-5005, GO-2026-5006, GO-2026-5013, GO-2026-5014, GO-2026-5015, GO-2026-5016, GO-2026-5017, GO-2026-5018, GO-2026-5019, GO-2026-5020, GO-2026-5021, GO-2026-5023, GO-2026-5033, GO-2026-5932, GO-2026-6303, GO-2026-6354, GO-2026-6355 |
| Go | `golang.org/x/mod` | 0.35.0 | GO-2026-6179, GO-2026-6180 |
| Go | `golang.org/x/net` | 0.53.0 | GO-2026-5025, GO-2026-5026, GO-2026-5027, GO-2026-5028, GO-2026-5029, GO-2026-5030, GO-2026-5942 |
| Go | `golang.org/x/sys` | 0.43.0 | GO-2026-5024 |
| Go | `golang.org/x/text` | 0.36.0 | GO-2026-5970 |
| Go | `google.golang.org/grpc` | 1.81.0 | GO-2026-6061, GHSA-vp52-pcj8-j9qc |
| npm | `@babel/core` | 7.29.0 | GHSA-4x5r-pxfx-6jf8 |
| npm | `@humanfs/node` | 0.16.7 | GHSA-p498-v437-472g |
| npm | `brace-expansion` | 2.1.1 | GHSA-3jxr-9vmj-r5cp, GHSA-mh99-v99m-4gvg, GHSA-rgw5-rvv9-x895 |
| npm | `browserslist` | 4.28.1 | GHSA-73wf-gq98-2v4g, GHSA-c83g-rgw3-j3cx |
| npm | `js-yaml` | 4.1.1 | GHSA-52cp-r559-cp3m, GHSA-5p4m-2wfm-xmqj, GHSA-h67p-54hq-rp68 |
| npm | `nanoid` | 3.3.11 | GHSA-28wg-ghj8-5hjv, GHSA-2v37-7h3g-55p8, GHSA-xwg4-73v4-xw9w |
| npm | `postcss` | 8.5.10 | GHSA-6g55-p6wh-862q, GHSA-fxqj-rqcc-2cmp, GHSA-r28c-9q8g-f849 |
| npm | `undici` | 7.28.0 | GHSA-4cwx-7wf7-3272, GHSA-8xcm-r25x-g524, GHSA-jr45-8vmc-qm54, GHSA-m8rv-5g2x-5cg5, GHSA-v3r7-h72x-cjcm |

Remove an entry when its lockfile no longer reports the advisory. The desired
end state is for `task security:audit STRICT=1` to pass without exceptions.
