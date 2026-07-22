# Golden E2E Harness

The three external-GitOps entry points are:

```text
make demo-bootstrap-pr
make scenario-open-regression-pr
make e2e-gitops
```

`demo-bootstrap-pr` requires explicit `GOLDEN_DEMO_IMAGE_DIGEST`,
`GOLDEN_DEMO_SOURCE_REVISION`, and a clean external GitOps checkout in
`GOLDEN_GITOPS_WORKTREE`. It also requires a clean CloudOps source worktree at
that exact revision, the fixed source/GitOps origins, GitOps `HEAD` equal to
`origin/main`, an empty base `apps/demo`, an authenticated human repository
owner, and a remotely inspectable final GHCR digest whose OCI source/revision
labels match CloudOps. It creates only `bootstrap/demo-manifests`, adds the
fixed five single-document manifests, commits, pushes, and opens a draft PR. It
never merges.

`scenario-open-regression-pr` requires a clean external GitOps checkout in
`GOLDEN_GITOPS_WORKTREE`. It uses the current human `gh` identity to create the
fixed `REQUIRED_ENV` removal branch and PR. It never merges the PR.

`e2e-gitops` is fail closed. It requires live GitHub Actions, separate
GitHub read/write App installation-token files, a kind context, a read-only
Argo token, an HTTPS real-model endpoint/key file, a real oauth2-proxy browser
cookie jar, exact digest-pinned API/Worker images, and the merged regression PR
number in `GOLDEN_REGRESSION_PR`. Provider credentials are read from explicit
files and are never written into evidence.

The GitHub App preflight verifies actual bounded capabilities without creating
remote state. Both tokens must read repository contents and pull requests. It
then submits a ref request bound to the impossible all-zero commit, an invalid
empty pull request, and an invalid empty branch-protection request. The read
token must receive `403`; the write token must receive `422` for Contents/PR
operations; both must receive `403` for repository administration. A final
read proves that neither the probe ref nor a probe PR exists.

Environment-specific inputs:

- `GOLDEN_GITOPS_WORKTREE`
- `GOLDEN_DEMO_IMAGE_DIGEST` and `GOLDEN_DEMO_SOURCE_REVISION` for
  `demo-bootstrap-pr`
- `GOLDEN_REGRESSION_PR`
- `GOLDEN_GITHUB_READ_APP_TOKEN_FILE`
- `GOLDEN_GITHUB_WRITE_APP_TOKEN_FILE`
- `GOLDEN_ARGO_SERVER` and `GOLDEN_ARGO_TOKEN_FILE`
- `GOLDEN_LLM_PROVIDER`, `GOLDEN_LLM_MODEL`, `GOLDEN_LLM_API_URL`, and
  `GOLDEN_LLM_API_KEY_FILE`
- `GOLDEN_API_BASE_URL` and `GOLDEN_OAUTH_COOKIE_JAR`

Optional names, timeouts, repository coordinates, and an alternate passing
Agent Quality report use the `GOLDEN_*` variables declared at the top of the
runner. No variable defaults to `111.txt`.

Every execution writes `docs/evidence/<exact-sha>/manifest.md`, including
literal PASS/FAIL/NOT RUN results, exact SHAs and image digests, GitHub Actions
URLs/conclusions, Argo revisions, public Incident-chain IDs, approval hashes,
model identity/hashes, usage, measured phase durations, resource evidence,
versions, commands, and known limitations. An existing manifest is preserved
unless `GOLDEN_OVERWRITE_EVIDENCE=true` is explicitly set.

Run `make golden-e2e-contracts` for the no-network, no-cluster-mutation shell
contract check.
