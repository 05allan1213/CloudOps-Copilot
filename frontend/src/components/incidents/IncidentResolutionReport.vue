<script setup lang="ts">
import { formatDurationMS } from "../../models/workbench";
import type { LoadState, ResolutionReportView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import JSONSnapshot from "./JSONSnapshot.vue";

defineProps<{ state: LoadState; error: string; report: ResolutionReportView | null }>();
</script>

<template>
  <IncidentSectionShell
    id="resolution-report"
    title="Resolution report"
    :state="state"
    :error="error"
    empty-text="No immutable ResolutionReport exists for the active cycle."
  >
    <article
      v-if="report"
      class="report"
    >
      <header class="report__header">
        <div>
          <span class="eyebrow">ResolutionReport {{ report.id }}</span>
          <h3>{{ report.summary }}</h3>
          <p>{{ report.resolution_reason }} · {{ report.trigger_type.replace(/_/g, " ") }}</p>
        </div>
        <IncidentStatusBadge :status="report.status" />
      </header>

      <div class="impact">
        <strong>Measured impact</strong>
        <p>{{ report.impact_summary }}</p>
      </div>

      <dl class="fact-grid">
        <div><dt>Service / workload</dt><dd>{{ report.service }} / {{ report.workload }}</dd></div>
        <div><dt>Environment</dt><dd>{{ report.environment }}</dd></div>
        <div><dt>Cycle started</dt><dd>{{ formatIncidentTime(report.cycle_started_at) }}</dd></div>
        <div><dt>Resolved</dt><dd>{{ formatIncidentTime(report.resolved_at) }}</dd></div>
        <div><dt>Measured duration</dt><dd>{{ formatDurationMS(report.measured_duration_ms) }}</dd></div>
        <div><dt>Generated</dt><dd>{{ formatIncidentTime(report.generated_at) }}</dd></div>
        <div><dt>Stable window started</dt><dd>{{ formatIncidentTime(report.stability.common_window_started_at) }}</dd></div>
        <div><dt>Stable window completed</dt><dd>{{ formatIncidentTime(report.stability.common_window_completed_at) }}</dd></div>
      </dl>

      <section
        class="identity-block"
        aria-labelledby="resolution-identities"
      >
        <h4 id="resolution-identities">
          Immutable report identity
        </h4>
        <HashValue
          label="Report hash"
          :value="report.hash"
        />
        <HashValue
          label="Verification profile"
          :value="report.verification_profile.hash"
        />
        <HashValue
          label="Bad GitOps revision"
          :value="report.revisions.bad_gitops_revision"
        />
        <HashValue
          label="Fix GitOps revision"
          :value="report.revisions.fix_gitops_revision"
        />
        <HashValue
          label="Source revision"
          :value="report.revisions.source_revision"
        />
        <HashValue
          label="Image digest"
          :value="report.revisions.image_digest"
        />
        <HashValue
          label="Deployed GitOps revision"
          :value="report.revisions.gitops_revision"
        />
      </section>

      <section
        class="report-sections"
        aria-labelledby="resolution-sections"
      >
        <div class="section-heading">
          <h4 id="resolution-sections">
            Auditable persisted sections
          </h4>
        </div>
        <JSONSnapshot
          title="Trigger signal"
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
          title="Remediation plan"
          :value="report.remediation_plan"
        />
        <JSONSnapshot
          title="Remediation decision"
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
          title="Agent usage"
          :value="report.agent_usage"
        />
      </section>
    </article>
  </IncidentSectionShell>
</template>

<style scoped>
.report { display: grid; gap: 20px; min-width: 0; }
.report__header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.report__header h3 { margin: 4px 0; }
.report__header p { margin: 0; color: var(--el-text-color-secondary); text-transform: capitalize; }
.eyebrow, dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .04em; }
.impact { padding: 14px; border-left: 4px solid var(--el-color-success); background: var(--el-color-success-light-9); }
.impact p { margin: 5px 0 0; }
.fact-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin: 0; }
.fact-grid div { min-width: 0; padding-bottom: 9px; border-bottom: 1px solid var(--cloudops-border-color); }
dd { margin: 4px 0 0; overflow-wrap: anywhere; }
.identity-block h4 { margin: 0 0 8px; }
.report-sections { display: grid; }
.section-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 8px; }
.section-heading h4 { margin: 0; }
@media (max-width: 640px) { .report__header, .section-heading { align-items: flex-start; flex-direction: column; } }
</style>
