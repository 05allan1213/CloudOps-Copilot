<script setup lang="ts">
import { computed, onMounted } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { useRoute } from "vue-router";

import IncidentHeader from "../../components/incidents/IncidentHeader.vue";
import IncidentResourceList from "../../components/incidents/IncidentResourceList.vue";
import IncidentSectionShell from "../../components/incidents/IncidentSectionShell.vue";
import { useIncidentDetail } from "../../composables/incidents/useIncidentDetail";
import { useIncidentRealtime } from "../../composables/incidents/useIncidentRealtime";
import { useAuthStore } from "../../stores/auth";
import type { ResourceView } from "../../types/incidents";

const route = useRoute();
const auth = useAuthStore();
const incidentID = String(route.params.incidentId ?? "");
const detail = useIncidentDetail(incidentID);
const realtime = useIncidentRealtime(incidentID, async () => detail.load());

const deliveryItems = computed<ResourceView[]>(() => detail.delivery.data ? [detail.delivery.data] : []);
const reportItems = computed<ResourceView[]>(() => detail.resolutionReport.data ? [detail.resolutionReport.data] : []);

onMounted(async () => {
  await detail.load();
  if (detail.pageState.value === "ready") realtime.start();
});

async function promptReason(title: string, required: boolean, maxLength = 2048): Promise<string | null> {
  try {
    const result = await ElMessageBox.prompt("This reason is persisted with the V3 command.", title, {
      confirmButtonText: "Submit",
      cancelButtonText: "Cancel",
      inputType: "textarea",
      inputPlaceholder: required ? "Required reason" : "Optional bounded reason",
      inputValidator: (value) => {
        const text = value.trim();
        if (required && !text) return "A reason is required";
        if (text.length > maxLength) return `Reason must be at most ${maxLength} characters`;
        return true;
      },
    });
    return result.value.trim();
  } catch {
    return null;
  }
}

async function runInvestigation() {
  const reason = await promptReason("Start a bounded re-investigation", false);
  if (reason === null) return;
  await runCommand(async () => detail.investigate(reason, await auth.commandToken()), "Investigation command accepted");
}

async function runClose() {
  const reason = await promptReason("Close this Incident", true);
  if (reason === null) return;
  await runCommand(async () => detail.close(reason, await auth.commandToken()), "Close command accepted");
}

async function runDecision(plan: ResourceView, decision: "approved" | "rejected") {
  const reason = await promptReason(`${decision === "approved" ? "Approve" : "Reject"} exact plan`, true, 1024);
  if (reason === null) return;
  await runCommand(async () => detail.decide(plan, decision, reason, await auth.commandToken()), `Plan ${decision}`);
}

async function runCommand(request: () => Promise<unknown>, success: string) {
  try {
    await request();
    ElMessage.success(success);
  } catch (cause) {
    ElMessage.error(cause instanceof Error ? cause.message : "Command failed");
  }
}
</script>

<template>
  <main class="incident-detail-view">
    <router-link
      :to="{ name: 'incidents' }"
      class="back-link"
    >
      ← All incidents
    </router-link>
    <div
      v-if="detail.pageState.value === 'loading'"
      role="status"
      aria-live="polite"
      class="page-message"
    >
      Loading incident…
    </div>
    <div
      v-else-if="detail.pageState.value === 'forbidden'"
      role="alert"
      class="page-message"
    >
      Viewer access is required.
    </div>
    <div
      v-else-if="detail.pageState.value === 'not_found'"
      role="status"
      class="page-message"
    >
      Incident not found.
    </div>
    <div
      v-else-if="detail.pageState.value !== 'ready' || !detail.incident.value"
      role="alert"
      class="page-message"
    >
      {{ detail.pageError.value || "Incident unavailable" }}
      <el-button @click="detail.load">
        Retry
      </el-button>
    </div>
    <template v-else>
      <IncidentHeader
        :incident="detail.incident.value"
        :realtime-state="realtime.state.value"
      />
      <div
        v-if="auth.isOperator"
        class="command-bar"
        aria-label="Operator commands"
      >
        <div>
          <strong>Operator commands</strong>
          <span>Server-side version, transition, CSRF and idempotency gates remain authoritative.</span>
        </div>
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
          Close
        </el-button>
      </div>
      <nav
        aria-label="Incident detail zones"
        class="section-nav"
      >
        <a href="#what-happened">What happened</a>
        <a href="#investigation-zone">Investigation</a>
        <a href="#remediation-delivery">Remediation &amp; Delivery</a>
        <a href="#recovery">Recovery</a>
      </nav>
      <div class="zone-stack">
        <section
          id="what-happened"
          class="zone"
        >
          <header><span>01</span><div><h2>What happened</h2><p>Signals and Timeline are persisted MySQL facts; the browser does not query Kubernetes.</p></div></header>
          <IncidentResourceList
            id="signals"
            title="Signals"
            :state="detail.signals.state"
            :error="detail.signals.error"
            :items="detail.signals.data"
            :next-cursor="detail.signals.nextCursor"
            @load-more="detail.moreSignals"
          />
          <IncidentResourceList
            id="timeline"
            title="Timeline"
            :state="detail.timeline.state"
            :error="detail.timeline.error"
            :items="detail.timeline.data"
            :next-cursor="detail.timeline.nextCursor"
            @load-more="detail.moreTimeline"
          />
        </section>
        <section
          id="investigation-zone"
          class="zone"
        >
          <header><span>02</span><div><h2>Investigation</h2><p>Bounded Agent runs, Diagnosis summaries and Evidence authority remain server projections.</p></div></header>
          <IncidentResourceList
            id="investigations"
            title="Investigations"
            :state="detail.investigations.state"
            :error="detail.investigations.error"
            :items="detail.investigations.data"
            :next-cursor="detail.investigations.nextCursor"
            @load-more="detail.moreInvestigations"
          />
          <IncidentResourceList
            id="evidence"
            title="Evidence"
            :state="detail.evidence.state"
            :error="detail.evidence.error"
            :items="detail.evidence.data"
            :next-cursor="detail.evidence.nextCursor"
            @load-more="detail.moreEvidence"
          />
        </section>
        <section
          id="remediation-delivery"
          class="zone"
        >
          <header><span>03</span><div><h2>Remediation &amp; Delivery</h2><p>Viewer sees the persisted canonical Plan hash and Decision state; only operator can submit Approve/Reject.</p></div></header>
          <IncidentResourceList
            id="remediation-plans"
            title="Remediation plans"
            :state="detail.remediationPlans.state"
            :error="detail.remediationPlans.error"
            :items="detail.remediationPlans.data"
            :next-cursor="detail.remediationPlans.nextCursor"
            @load-more="detail.moreRemediationPlans"
          >
            <template #actions="{ item }">
              <div
                v-if="auth.isOperator && item.status === 'awaiting_approval'"
                class="plan-actions"
              >
                <el-button
                  type="success"
                  :disabled="!item.version || !item.hash"
                  :loading="detail.commandPending.value"
                  @click="runDecision(item, 'approved')"
                >
                  Approve exact hash
                </el-button>
                <el-button
                  type="danger"
                  plain
                  :disabled="!item.version || !item.hash"
                  :loading="detail.commandPending.value"
                  @click="runDecision(item, 'rejected')"
                >
                  Reject
                </el-button>
              </div>
            </template>
          </IncidentResourceList>
          <IncidentResourceList
            id="delivery"
            title="Delivery"
            :state="detail.delivery.state"
            :error="detail.delivery.error"
            :items="deliveryItems"
            empty-text="No ChangeRequest has been projected."
          />
        </section>
        <section
          id="recovery"
          class="zone"
        >
          <header><span>04</span><div><h2>Recovery</h2><p>Verification verdicts and the immutable ResolutionReport are displayed without browser recomputation.</p></div></header>
          <IncidentResourceList
            id="verifications"
            title="Verification runs"
            :state="detail.verifications.state"
            :error="detail.verifications.error"
            :items="detail.verifications.data"
            :next-cursor="detail.verifications.nextCursor"
            @load-more="detail.moreVerifications"
          />
          <IncidentResourceList
            id="resolution-report"
            title="Resolution report"
            :state="detail.resolutionReport.state"
            :error="detail.resolutionReport.error"
            :items="reportItems"
            empty-text="No immutable ResolutionReport exists for the active cycle."
          />
        </section>
      </div>
      <IncidentSectionShell
        id="authority-note"
        title="Workbench authority boundary"
        state="ready"
      >
        <p class="authority-note">
          This page uses only /api/v3 Query, SSE and the three documented Command families. It stores no OAuth credential or CSRF token outside process memory and sends no Authorization header.
        </p>
      </IncidentSectionShell>
    </template>
  </main>
</template>

<style scoped>
.incident-detail-view { display: grid; gap: 16px; }
.back-link { width: fit-content; color: var(--el-color-primary); }
.page-message { padding: 50px; text-align: center; }
.command-bar { display: flex; align-items: center; gap: 10px; padding: 14px; border: 1px solid var(--el-color-warning-light-5); border-radius: 9px; background: var(--el-color-warning-light-9); }
.command-bar > div { display: grid; gap: 3px; margin-right: auto; }
.command-bar span { color: var(--el-text-color-secondary); font-size: 12px; }
.section-nav { position: sticky; top: 0; z-index: 5; display: flex; gap: 6px; overflow-x: auto; padding: 10px; border: 1px solid var(--cloudops-border-color); border-radius: 9px; background: var(--cloudops-bg-card); }
.section-nav a { padding: 6px 9px; color: var(--el-text-color-regular); text-decoration: none; border-radius: 5px; white-space: nowrap; }
.section-nav a:hover, .section-nav a:focus-visible { background: var(--el-fill-color-light); outline: 2px solid var(--el-color-primary); }
.zone-stack { display: grid; gap: 22px; }
.zone { display: grid; gap: 14px; scroll-margin-top: 72px; }
.zone > header { display: flex; gap: 12px; align-items: start; padding: 4px 2px; }
.zone > header > span { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 50%; color: white; background: var(--el-color-primary); font-size: 12px; font-weight: 700; }
.zone h2, .zone p { margin: 0; }
.zone p { margin-top: 4px; color: var(--el-text-color-secondary); }
.plan-actions { display: flex; gap: 8px; margin-top: 14px; }
.authority-note { margin: 0; color: var(--el-text-color-secondary); }
@media (max-width: 760px) { .command-bar { align-items: stretch; flex-direction: column; } .command-bar > div { margin-right: 0; } }
</style>
