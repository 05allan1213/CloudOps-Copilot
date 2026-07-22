<script setup lang="ts">
import { computed, onMounted } from "vue";
import { ArrowLeft } from "@element-plus/icons-vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { useRoute, useRouter } from "vue-router";

import ActivityTimeline from "../../components/incidents/ActivityTimeline.vue";
import AgentActivityPanel from "../../components/incidents/AgentActivityPanel.vue";
import CommandFeedback from "../../components/incidents/CommandFeedback.vue";
import DeliveryRail from "../../components/incidents/DeliveryRail.vue";
import EvidenceTable from "../../components/incidents/EvidenceTable.vue";
import IncidentHeader from "../../components/incidents/IncidentHeader.vue";
import IncidentSignalStrip from "../../components/incidents/IncidentSignalStrip.vue";
import PersistedContextPanel from "../../components/incidents/PersistedContextPanel.vue";
import RemediationWorkbench from "../../components/incidents/RemediationWorkbench.vue";
import ResolutionReport from "../../components/incidents/ResolutionReport.vue";
import StateBlock from "../../components/incidents/StateBlock.vue";
import VerificationMatrix from "../../components/incidents/VerificationMatrix.vue";
import ZoneNav, { type IncidentZone } from "../../components/incidents/ZoneNav.vue";
import { useIncidentDetail } from "../../composables/incidents/useIncidentDetail";
import { useIncidentRealtime } from "../../composables/incidents/useIncidentRealtime";
import { canExposeResolutionReport } from "../../models/recovery";
import { useAuthStore } from "../../stores/auth";
import type { IncidentRealtimeEvent, RemediationPlanView } from "../../types/incidents";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const incidentID = String(route.params.incidentId ?? "");
const detail = useIncidentDetail(incidentID);
const realtime = useIncidentRealtime(incidentID, detail.refreshResource);
const resolutionEligible = computed(() => canExposeResolutionReport(detail.verifications.data));
const incidentCommandFeedback = computed(() => detail.commandFeedback.value?.resourceID === incidentID
  ? detail.commandFeedback.value
  : null);
const zones: IncidentZone[] = [
  { id: "what-happened", label: "What Happened", index: "01" },
  { id: "investigation-zone", label: "Investigation", index: "02" },
  { id: "remediation-delivery", label: "Remediation & Delivery", index: "03" },
  { id: "recovery", label: "Recovery", index: "04" },
];

onMounted(loadDetail);

async function loadDetail() {
  await detail.load();
  if (detail.pageState.value === "ready" && realtime.state.value === "disconnected") realtime.start();
}

function retryResource(resource: IncidentRealtimeEvent["resource"]) {
  void detail.refreshResource(resource).catch(() => {
    // The section retains visible data and renders the normalized request error.
  });
}

function returnToIncidents() {
  void router.push({ name: "incidents" });
}

async function promptReason(title: string, required: boolean, maxLength = 2048): Promise<string | null> {
  try {
    const result = await ElMessageBox.prompt("This reason is persisted with the V3 command.", title, {
      confirmButtonText: "Submit Command",
      cancelButtonText: "Cancel",
      inputType: "textarea",
      inputPlaceholder: required ? "Example: exact evidence supporting this decision…" : "Optional bounded reason…",
      inputValidator: (value) => {
        const text = value.trim();
        if (required && !text) return "Enter a reason before submitting this command.";
        if (text.length > maxLength) return `Keep the reason within ${maxLength} characters.`;
        return true;
      },
    });
    return result.value.trim();
  } catch {
    return null;
  }
}

async function runInvestigation() {
  const reason = await promptReason("Start Bounded Re-investigation", false, 1024);
  if (reason === null) return;
  await runIncidentCommand(async () => detail.investigate(reason, await auth.commandToken()));
}

async function runClose() {
  const reason = await promptReason("Close This Incident", true);
  if (reason === null) return;
  await runIncidentCommand(async () => detail.close(reason, await auth.commandToken()));
}

async function runDecision(plan: RemediationPlanView, decision: "approved" | "rejected", reason: string) {
  try {
    await detail.decide(plan, decision, reason, await auth.commandToken());
  } catch (cause) {
    if (detail.commandFeedback.value?.resourceID !== plan.id) {
      ElMessage.error(cause instanceof Error ? cause.message : "Decision command failed before submission");
    }
  }
}

async function runIncidentCommand(request: () => Promise<unknown>) {
  try {
    await request();
  } catch (cause) {
    if (!incidentCommandFeedback.value) {
      ElMessage.error(cause instanceof Error ? cause.message : "Command failed before submission");
    }
  }
}

async function retryCommand() {
  try {
    await detail.retryLastCommand();
  } catch {
    // CommandFeedback preserves the retry result and request identity.
  }
}

async function refreshRemediationAfterConflict() {
  try {
    await detail.refreshResource("remediation_plans");
    detail.clearCommandFeedback();
  } catch {
    // The section keeps the prior Plan visible and renders the refresh error.
  }
}

async function refreshIncidentAfterConflict() {
  try {
    await detail.load({ preserve: true });
    detail.clearCommandFeedback();
  } catch {
    // Detail load normalizes the error while preserving the visible Incident.
  }
}
</script>

<template>
  <section class="incident-detail-view">
    <router-link
      :to="{ name: 'incidents' }"
      class="back-link"
    >
      <el-icon aria-hidden="true">
        <ArrowLeft />
      </el-icon>
      All Incidents
    </router-link>

    <template v-if="detail.pageState.value !== 'ready' || !detail.incident.value">
      <h1 class="visually-hidden">
        Incident Detail
      </h1>
      <StateBlock
        :state="detail.pageState.value === 'ready' ? 'error' : detail.pageState.value"
        :title="detail.pageState.value === 'loading' ? 'Loading Incident' : undefined"
        :message="detail.pageError.value?.message"
        :request-i-d="detail.pageError.value?.requestID"
        :trace-i-d="detail.pageError.value?.traceID"
        :primary-action-label="['error', 'unavailable'].includes(detail.pageState.value) ? 'Retry Incident' : undefined"
        secondary-action-label="Back to Incidents"
        @primary-action="loadDetail"
        @secondary-action="returnToIncidents"
      />
    </template>

    <template v-else>
      <IncidentHeader
        :incident="detail.incident.value"
        :realtime-state="realtime.state.value"
        :realtime-notice="realtime.notice.value"
        :refreshing="detail.refreshing.value"
        :last-updated-at="detail.lastUpdatedAt.value"
      />

      <div
        v-if="detail.pageError.value"
        class="page-refresh-warning"
        role="status"
        aria-live="polite"
      >
        <strong>The visible Incident remains available.</strong>
        <span>{{ detail.pageError.value.message }}</span>
        <code
          v-if="detail.pageError.value.requestID"
          translate="no"
        >Request {{ detail.pageError.value.requestID }}</code>
      </div>

      <div
        v-if="auth.isOperator"
        class="command-bar"
        aria-label="Operator Incident commands"
      >
        <div>
          <strong>Operator Commands</strong>
          <span>Server-side version, transition, CSRF, and idempotency checks remain authoritative.</span>
        </div>
        <div class="command-actions">
          <el-button
            :loading="detail.commandPending.value"
            @click="runInvestigation"
          >
            Re-investigate
          </el-button>
          <el-button
            type="danger"
            plain
            :loading="detail.commandPending.value"
            @click="runClose"
          >
            Close Incident
          </el-button>
        </div>
      </div>

      <CommandFeedback
        :feedback="incidentCommandFeedback"
        :pending="detail.commandPending.value"
        @retry="retryCommand"
        @refresh="refreshIncidentAfterConflict"
      />

      <ZoneNav :zones="zones" />

      <div class="zone-stack">
        <section
          id="what-happened"
          class="incident-zone"
          aria-labelledby="what-happened-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">01</span>
            <div>
              <h2 id="what-happened-title">
                What Happened
              </h2>
              <p>Persisted signals, runtime context, and server-ordered Timeline facts define the observable incident history.</p>
            </div>
          </header>

          <IncidentSignalStrip
            :state="detail.signals.state"
            :error="detail.signals.error"
            :items="detail.signals.data"
            :next-cursor="detail.signals.nextCursor"
            :refreshing="detail.signals.refreshing"
            :loading-more="detail.signals.loadingMore"
            @load-more="detail.moreSignals"
            @retry="retryResource('signals')"
          />

          <PersistedContextPanel
            :signals="detail.signals.data"
            :evidence="detail.evidence.data"
            :timeline="detail.timeline.data"
          />

          <ActivityTimeline
            :state="detail.timeline.state"
            :error="detail.timeline.error"
            :items="detail.timeline.data"
            :next-cursor="detail.timeline.nextCursor"
            :refreshing="detail.timeline.refreshing"
            :loading-more="detail.timeline.loadingMore"
            @load-more="detail.moreTimeline"
            @retry="retryResource('timeline')"
          />
        </section>

        <section
          id="investigation-zone"
          class="incident-zone"
          aria-labelledby="investigation-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">02</span>
            <div>
              <h2 id="investigation-zone-title">
                Investigation
              </h2>
              <p>Bounded Investigation and Evidence projections are readable without exposing private reasoning or inventing missing relationships.</p>
            </div>
          </header>

          <AgentActivityPanel
            :state="detail.investigations.state"
            :error="detail.investigations.error"
            :items="detail.investigations.data"
            :next-cursor="detail.investigations.nextCursor"
            :refreshing="detail.investigations.refreshing"
            :loading-more="detail.investigations.loadingMore"
            @load-more="detail.moreInvestigations"
            @retry="retryResource('investigations')"
          />

          <EvidenceTable
            :state="detail.evidence.state"
            :error="detail.evidence.error"
            :items="detail.evidence.data"
            :next-cursor="detail.evidence.nextCursor"
            :refreshing="detail.evidence.refreshing"
            :loading-more="detail.evidence.loadingMore"
            @load-more="detail.moreEvidence"
            @retry="retryResource('evidence')"
          />
        </section>

        <section
          id="remediation-delivery"
          class="incident-zone"
          aria-labelledby="remediation-delivery-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">03</span>
            <div>
              <h2 id="remediation-delivery-title">
                Remediation &amp; Delivery
              </h2>
              <p>The exact persisted Plan, Decision, and delivery identities remain separate from recovery verification.</p>
            </div>
          </header>

          <RemediationWorkbench
            :state="detail.remediationPlans.state"
            :error="detail.remediationPlans.error"
            :plans="detail.remediationPlans.data"
            :next-cursor="detail.remediationPlans.nextCursor"
            :refreshing="detail.remediationPlans.refreshing"
            :loading-more="detail.remediationPlans.loadingMore"
            :incident-version="detail.incident.value.version"
            :incident-status="detail.incident.value.status"
            :is-operator="auth.isOperator"
            :command-pending="detail.commandPending.value"
            :command-feedback="detail.commandFeedback.value"
            @load-more="detail.moreRemediationPlans"
            @decide="runDecision"
            @retry-resource="retryResource('remediation_plans')"
            @retry-command="retryCommand"
            @refresh-conflict="refreshRemediationAfterConflict"
          />
          <DeliveryRail
            :state="detail.delivery.state"
            :error="detail.delivery.error"
            :delivery="detail.delivery.data"
            :refreshing="detail.delivery.refreshing"
            @retry="retryResource('delivery')"
          />
        </section>

        <section
          id="recovery"
          class="incident-zone"
          aria-labelledby="recovery-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">04</span>
            <div>
              <h2 id="recovery-title">
                Recovery
              </h2>
              <p>Verification outcomes remain distinct from delivery health, and only a persisted Resolution Report establishes resolution.</p>
            </div>
          </header>

          <VerificationMatrix
            :state="detail.verifications.state"
            :error="detail.verifications.error"
            :runs="detail.verifications.data"
            :next-cursor="detail.verifications.nextCursor"
            :refreshing="detail.verifications.refreshing"
            :loading-more="detail.verifications.loadingMore"
            @load-more="detail.moreVerifications"
            @retry="retryResource('verifications')"
          />
          <ResolutionReport
            :state="detail.resolutionReport.state"
            :error="detail.resolutionReport.error"
            :report="detail.resolutionReport.data"
            :eligible="resolutionEligible"
            :refreshing="detail.resolutionReport.refreshing"
            @retry="retryResource('resolution_report')"
          />
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.incident-detail-view {
  display: grid;
  width: min(100%, var(--co-content-max-width));
  min-width: 0;
  margin: 0 auto;
  gap: var(--co-space-4);
}

.back-link {
  display: inline-flex;
  width: fit-content;
  min-height: 40px;
  align-items: center;
  gap: var(--co-space-2);
  color: var(--co-action-primary);
  font-size: 13px;
  font-weight: 700;
}

.back-link:hover { color: var(--co-action-hover); text-decoration: underline; text-underline-offset: 3px; }

.page-refresh-warning {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-2) var(--co-space-4);
  padding: var(--co-space-3) var(--co-space-4);
  border-left: 3px solid var(--co-status-warning-fg);
  color: var(--co-status-warning-fg);
  background: var(--co-status-warning-bg);
  font-size: 12px;
}

.page-refresh-warning code { overflow-wrap: anywhere; }

.command-bar {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--co-space-4);
  padding: var(--co-space-3) 0;
  border-bottom: 1px solid var(--co-border-default);
}

.command-bar > div:first-child {
  display: grid;
  min-width: 0;
  gap: 2px;
  margin-right: auto;
}

.command-bar span { color: var(--co-text-muted); font-size: 12px; }
.command-actions { display: flex; flex: 0 0 auto; gap: var(--co-space-2); }

.zone-stack {
  display: grid;
  min-width: 0;
  gap: var(--co-space-10);
}

.incident-zone {
  display: grid;
  min-width: 0;
  gap: 0;
  scroll-margin-top: 64px;
}

.zone-heading {
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  min-width: 0;
  gap: var(--co-space-3);
  padding: var(--co-space-6) 0 var(--co-space-2);
}

.zone-heading > span {
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  border: 1px solid var(--co-border-strong);
  border-radius: var(--co-radius-pill);
  color: var(--co-text-muted);
  background: var(--co-bg-surface);
  font-family: var(--co-font-mono);
  font-size: 11px;
  font-weight: 700;
}

.zone-heading h2,
.zone-heading p { margin: 0; }
.zone-heading h2 { color: var(--co-text-primary); font-size: 21px; }
.zone-heading p { max-width: 82ch; margin-top: 3px; color: var(--co-text-secondary); font-size: 13px; }

@media (max-width: 767px) {
  .back-link { min-height: 44px; }
  .command-bar { align-items: stretch; flex-direction: column; }
  .command-bar > div:first-child { margin-right: 0; }
  .command-actions { display: grid; grid-template-columns: minmax(0, 1fr); }
  .command-actions :deep(.el-button) { width: 100%; margin-left: 0; }
  .zone-stack { gap: var(--co-space-8); }
  .zone-heading { padding-top: var(--co-space-5); }
}
</style>
