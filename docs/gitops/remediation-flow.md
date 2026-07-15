# GitOps Remediation and Verification Flow

```text
human-approved typed remediation
  -> Draft PR / CI / external merge
  -> exact merged SHA
  -> Argo CD exact deployed revision
  -> Deployment stable
  -> alert + fixed-template metric/log/trace checks
  -> continuous stability windows
  -> deterministic aggregate
  -> Incident RESOLVED + Timeline + Outbox + Postmortem (one transaction)
```

All GitHub, Argo CD, Kubernetes and observability reads occur outside MySQL transactions. GitHub and Argo CD are GET-only; Kubernetes is typed Deployment GET/Pod LIST only; observability adapters are fixed-host GET-only. Phase 6 adds no merge, sync, rollback, refresh, patch, scale, restart, delete, exec, shell, kubectl or automatic revert authority.

Any required failure/invalid result or overall timeout atomically returns `VERIFYING` to `DIAGNOSING`. Provider unavailable never passes and no Agent loop is started automatically. Retry remains an append-only operator-controlled attempt.

