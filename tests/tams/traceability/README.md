# TAMS traceability

Requirement identity is capability-based and stable across TAMS releases. Release versions
are provenance, not part of a requirement ID or an enduring behaviour-test name.

- `requirements.yaml` is the current registry. It links each stable requirement to its
  implementation and test evidence, using exact Python and Go test nodes where those runners
  expose stable function names. `disposition` records whether TAMOSS implements the
  requirement; CI determines whether that evidence passes.
- `releases/<version>.yaml` is an immutable upstream delta. It pins the tag commits and maps
  every changed operation, schema, webhook, file, and commit to stable requirement IDs. It
  does not duplicate implementation or test references.
- `test_release_traceability.py` validates both layers and checks the latest release manifest
  against the vendored BBC TAMS revision. It also requires every conformance test module to
  belong to exactly one of the contract or semantic gates, preventing silent collection gaps.

Versions remain appropriate in tests whose behaviour is itself a version boundary, such as a
database upgrade from one frozen schema to another or advertised release compatibility.
