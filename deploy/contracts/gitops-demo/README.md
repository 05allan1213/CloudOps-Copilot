# GitOps Demo Contract Fixtures

These files are local, bounded contract fixtures for the dedicated
`05allan1213/cloudops-gitops-demo` repository. They are not applied by the
CloudOps demo bootstrap and are not a second live desired-state source.

- `healthy/apps/demo/` is the canonical healthy manifest shape expected at
  the external repository's `apps/demo` path.
- `regression/apps/demo/deployment.yaml` is the deterministic replacement used
  by the regression PR fixture. Its only semantic change from the healthy
  Deployment is removal of the non-secret `REQUIRED_ENV` entry.

Image and source-revision values use explicit `contract-fixture` sentinels.
The external GitOps repository must replace them with the exact release image
and revision while preserving the validated resource and policy shape.

The fixtures must never contain credentials, a load-generator Job, RBAC,
Secrets, cluster-scoped resources, or any resource outside the AppProject
allowlist.
