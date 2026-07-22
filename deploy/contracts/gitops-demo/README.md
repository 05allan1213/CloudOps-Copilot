# GitOps Demo Contract Fixtures

These files are local, bounded contract fixtures for the dedicated
`05allan1213/cloudops-gitops-demo` repository. They are not applied by the
CloudOps demo bootstrap and are not a second live desired-state source.

- `healthy/apps/demo/` is the canonical five-file healthy manifest shape
  expected at the external repository's `apps/demo` path. The inventory is
  exactly `deployment.yaml`, `diagnostics-service.yaml`, `podmonitor.yaml`,
  `prometheusrule.yaml`, and `service.yaml`; every file contains one YAML
  document.
- `regression/apps/demo/` repeats that exact inventory. Its only semantic
  change from the healthy tree is removal of the non-secret `REQUIRED_ENV`
  entry from `deployment.yaml`.

The checked-in image reference is an immutable digest from a locally inspected
Demo build, and its OCI revision/source labels bind it to the exact checked-in
source revision. It is a contract seed, not publication evidence. The
`make demo-bootstrap-pr` command always replaces both values and refuses to
publish unless the operator supplies a final GHCR digest whose remote manifest
and OCI labels match the clean CloudOps HEAD.

The fixtures must never contain credentials, a load-generator Job, RBAC,
Secrets, cluster-scoped resources, or any resource outside the AppProject
allowlist.
