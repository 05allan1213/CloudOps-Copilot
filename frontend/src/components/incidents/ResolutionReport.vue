<script setup lang="ts">
import { computed } from "vue";

import { formatDurationMS } from "../../models/workbench";
import type { LoadState, ResolutionReportView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import JSONSnapshot from "./JSONSnapshot.vue";
import ResultBadge from "./ResultBadge.vue";

interface SectionProblem {
  message: string;
  requestID?: string;
  traceID?: string;
}

const props = withDefaults(defineProps<{
  state: LoadState;
  error?: SectionProblem | null;
  report: ResolutionReportView | null;
  eligible: boolean;
  refreshing?: boolean;
}>(), {
  error: null,
  refreshing: false,
});

const emit = defineEmits<{ retry: [] }>();
const missingSections = computed(() => {
  if (!props.report) return [];
  const missing: string[] = [];
  if (props.report.diagnosis === null) missing.push("Diagnosis is not present in this report.");
  if (props.report.remediation_plan === null) missing.push("No remediation Plan is present; this may be a no-change recovery.");
  if (props.report.remediation_decision === null) missing.push("No remediation Decision is present.");
  if (props.report.delivery === null) missing.push("No Delivery is present; this report does not imply an external write.");
  return missing;
});
</script>

<template>
  <IncidentSectionShell
    id="resolution-report"
    title="Resolution Report"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :retryable="true"
    empty-text="No immutable ResolutionReport exists. Recovery is not presented as resolved."
    projection-note="The report is exposed only when the latest persisted VerificationRun is passed."
    @retry="emit('retry')"
  >
    <div
      v-if="report && !eligible"
      class="report-withheld"
      role="alert"
    >
      <strong>ResolutionReport withheld by the Workbench.</strong>
      <span>The current Verification projection is not passed. Refresh the server projections; the browser will not greenwash this mismatch.</span>
    </div>

    <article
      v-else-if="report"
      class="report"
    >
      <header class="report-header">
        <div>
          <span>ResolutionReport {{ report.id }}</span>
          <h3>{{ report.summary }}</h3>
          <p>{{ report.resolution_reason.replace(/_/g, " ") }} · {{ report.trigger_type.replace(/_/g, " ") }}</p>
        </div>
        <ResultBadge :result="report.status" />
      </header>

      <div
        v-if="report.trigger_type === 'no_change_signal'"
        class="no-change-banner"
        role="status"
      >
        <strong>No-change resolution</strong>
        <span>This report does not claim a Plan, approval, PR, Argo sync, or rollout unless those persisted sections are present.</span>
      </div>

      <section
        class="impact"
        aria-labelledby="resolution-impact"
      >
        <h4 id="resolution-impact">
          Measured Outcome
        </h4>
        <p>{{ report.impact_summary }}</p>
      </section>

      <dl class="report-facts">
        <div><dt>Service / Workload</dt><dd>{{ report.service }} / {{ report.workload }}</dd></div>
        <div><dt>Environment</dt><dd>{{ report.environment }}</dd></div>
        <div><dt>Cycle</dt><dd>{{ report.cycle }}</dd></div>
        <div><dt>Cycle Started</dt><dd>{{ formatIncidentTime(report.cycle_started_at) }}</dd></div>
        <div><dt>Resolved</dt><dd>{{ formatIncidentTime(report.resolved_at) }}</dd></div>
        <div><dt>Measured Duration</dt><dd>{{ formatDurationMS(report.measured_duration_ms) }}</dd></div>
        <div><dt>Generated</dt><dd>{{ formatIncidentTime(report.generated_at) }}</dd></div>
        <div><dt>Stable Window</dt><dd>{{ formatIncidentTime(report.stability.common_window_started_at) }} → {{ formatIncidentTime(report.stability.common_window_completed_at) }}</dd></div>
      </dl>

      <section
        class="identity-section"
        aria-labelledby="resolution-identities"
      >
        <h4 id="resolution-identities">
          Immutable Resolution Identity
        </h4>
        <div class="hash-grid">
          <HashValue
            label="Report Hash"
            :value="report.hash"
          />
          <HashValue
            label="Verification Profile"
            :value="report.verification_profile.hash"
          />
          <HashValue
            label="Bad GitOps Revision"
            :value="report.revisions.bad_gitops_revision"
          />
          <HashValue
            label="Fix GitOps Revision"
            :value="report.revisions.fix_gitops_revision"
          />
          <HashValue
            label="Source Revision"
            :value="report.revisions.source_revision"
          />
          <HashValue
            label="Image Digest"
            :value="report.revisions.image_digest"
          />
          <HashValue
            label="Deployed GitOps Revision"
            :value="report.revisions.gitops_revision"
          />
        </div>
      </section>

      <section
        class="limits"
        aria-labelledby="resolution-limits"
      >
        <h4 id="resolution-limits">
          Persisted Limits & Follow-ups
        </h4>
        <ul>
          <li
            v-for="item in missingSections"
            :key="item"
          >
            {{ item }}
          </li>
          <li v-if="report.migrated_legacy_context">
            This report includes migrated legacy context; provenance remains explicit.
          </li>
          <li>Follow-up actions are not projected by the current ResolutionReport contract.</li>
        </ul>
      </section>

      <details class="report-package">
        <summary>Inspect Auditable Persisted Sections</summary>
        <div>
          <JSONSnapshot
            title="Trigger Signal"
            :value="report.trigger_signal"
          />
          <JSONSnapshot
            title="Diagnosis"
            :value="report.diagnosis"
          />
          <JSONSnapshot
            title="Evidence"
            :value="report.evidence"
          />
          <JSONSnapshot
            title="Remediation Plan"
            :value="report.remediation_plan"
          />
          <JSONSnapshot
            title="Remediation Decision"
            :value="report.remediation_decision"
          />
          <JSONSnapshot
            title="Delivery"
            :value="report.delivery"
          />
          <JSONSnapshot
            title="Verification"
            :value="report.verification"
          />
          <JSONSnapshot
            title="Timeline"
            :value="report.timeline"
          />
          <JSONSnapshot
            title="Agent Usage"
            :value="report.agent_usage"
          />
        </div>
      </details>
    </article>
  </IncidentSectionShell>
</template>

<style scoped>
.report,
.identity-section,
.limits {
  display: grid;
  min-width: 0;
}

.report { gap: var(--co-space-5); padding: var(--co-space-5); border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.report-header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); }
.report-header > div { min-width: 0; }
.report-header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.report-header h3 { margin: 3px 0; color: var(--co-text-primary); font-size: 20px; overflow-wrap: anywhere; }
.report-header p { margin: 0; color: var(--co-text-secondary); text-transform: capitalize; }

.report-withheld,
.no-change-banner { display: grid; gap: 2px; padding: var(--co-space-3) var(--co-space-4); border-left: 3px solid var(--co-status-critical-fg); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.no-change-banner { border-left-color: var(--co-status-neutral-border); color: var(--co-text-secondary); background: var(--co-bg-subtle); }
.impact { padding: var(--co-space-4); border-left: 3px solid var(--co-status-success-fg); color: var(--co-text-secondary); background: var(--co-status-success-bg); }
.impact h4,
.impact p { margin: 0; }
.impact h4 { color: var(--co-status-success-fg); font-size: 14px; }
.impact p { margin-top: var(--co-space-1); }

.report-facts { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; }
.report-facts div { min-width: 0; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.report-facts dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.report-facts dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

.identity-section,
.limits { gap: var(--co-space-3); }
.identity-section > h4,
.limits h4 { margin: 0; color: var(--co-text-primary); font-size: 15px; }
.hash-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--co-space-5); }
.limits { padding: var(--co-space-4); border-left: 3px solid var(--co-status-neutral-border); background: var(--co-bg-subtle); }
.limits ul { display: grid; gap: var(--co-space-2); margin: 0; padding-left: var(--co-space-5); color: var(--co-text-secondary); }

.report-package { border-block: 1px solid var(--co-border-default); }
.report-package > summary { width: fit-content; min-height: 44px; padding: var(--co-space-3) 0; color: var(--co-action-primary); font-weight: 700; cursor: pointer; }
.report-package > div { padding-bottom: var(--co-space-4); }

@media (max-width: 980px) {
  .report-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 640px) {
  .report { padding: var(--co-space-4); }
  .report-header { align-items: flex-start; flex-direction: column; }
  .report-facts,
  .hash-grid { grid-template-columns: minmax(0, 1fr); }
}
</style>
