# Golden E2E Harness

The two public entry points are:

```text
make scenario-open-regression-pr
make e2e-gitops
```

The first command requires a clean external GitOps checkout in
`GOLDEN_GITOPS_WORKTREE`. It uses the current human `gh` identity to create the
fixed `REQUIRED_ENV` removal branch and PR. It never merges the PR.

The second command is fail closed. It requires live GitHub Actions, separate
GitHub read/write App installation-token files, a kind context, a read-only
Argo token, an HTTPS real-model endpoint/key file, a real oauth2-proxy browser
cookie jar, exact digest-pinned API/Worker images, and the merged regression PR
number in `GOLDEN_REGRESSION_PR`. Provider credentials are read from explicit
files and are never written into evidence.

Environment-specific inputs:

- `GOLDEN_GITOPS_WORKTREE`
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
