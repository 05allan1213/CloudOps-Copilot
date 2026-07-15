# Incident Lifecycle: Verification Closure

Phase 6 retains the Phase 1 Incident states and Phase 5 VerificationRun/VerificationCheck state machines. It does not introduce a parallel observability state machine.

Only the trusted profile compiler, deterministic evaluator and required-check aggregate own recovery authority. LLM output, Agent text, provider descriptions, API callers and Postmortem generation cannot set a verdict.

Each successful observation starts or continues its own stability window. Any non-success resets that check. Only when every required delivery, alert, metric, log and trace check is terminal passed can the aggregate transition `VERIFYING -> RESOLVED`. The same transaction stores the terminal Run, Incident resolution, Timeline/Outbox facts and one structured Postmortem.

Required failure or timeout transitions `VERIFYING -> DIAGNOSING` and retains durable check observations. Optional checks remain queryable but do not override the profile rule. Lease expiry permits a second worker to take over; optimistic row versions reject the old worker. Check persistence releases the lease so a crash before aggregation is safely replayed.

