# Observability Verification Telemetry Contract

Phase 6 emits bounded labels only. Provider labels are `prometheus`, `loki` or `tempo`; result/status/type labels are fixed enums. Incident text, IDs, service names, hosts, queries, tokens, DSNs, responses, log bodies, trace attributes, prompts and private reasoning are never metric labels or span attributes.

Metrics include `verification_provider_requests_total`, `verification_provider_failures_total`, `verification_provider_duration_seconds`, `verification_observability_checks_total`, `verification_stability_resets_total`, `postmortems_generated_total` and `postmortem_generation_failures_total` in addition to Phase 5 delivery/verification metrics.

Spans cover plan compilation, provider observations, check evaluation/persistence, Run aggregation, Postmortem generation and Postmortem query. HTTP instrumentation may record the fixed route but must not record provider query strings or Authorization headers.

Provider responses are reduced to bounded numeric/time facts. Loki examples, when a future template elects to retain them, are secret-redacted, individually truncated and count-limited. Current templates persist counts only. Tempo never persists Span attributes.

