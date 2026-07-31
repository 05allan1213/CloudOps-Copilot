<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { Bot, FileCheck2, GitPullRequest, ShieldCheck } from "lucide-vue-next";

import {
  getIncident,
  getIncidentDelivery,
  listIncidentEvidence,
  listIncidentInvestigations,
  listIncidentRemediationPlans,
  listIncidentVerifications,
} from "../../api/incidents";
import { apiErrorDetails } from "../../api/client";
import WorkspaceState from "../workspace/WorkspaceState.vue";
import type { WorkspaceStateKind } from "../workspace/workspacePresentation";
import {
  incidentInspectorFailureKind,
  incidentStatusLabel,
  severityLabel,
} from "../../models/incidents";
import { latestVerificationRun } from "../../models/recovery";
import type {
  DeliveryView,
  IncidentInvestigationView,
  IncidentView,
  RemediationPlanView,
  VerificationRunView,
} from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";

const props = defineProps<{ incidentID: string }>();

const incident = ref<IncidentView | null>(null);
const investigations = ref<IncidentInvestigationView[]>([]);
const evidenceCount = ref(0);
const plan = ref<RemediationPlanView | null>(null);
const delivery = ref<DeliveryView | null>(null);
const verification = ref<VerificationRunView | null>(null);
const loading = ref(false);
const failure = shallowRef<unknown>(null);
const requestIdentity = ref(0);
let controller: AbortController | null = null;

const failureDetails = computed(() => apiErrorDetails(failure.value, "Incident 摘要读取失败"));
const targetState = computed(() => incidentInspectorFailureKind(
  props.incidentID,
  failure.value ? failureDetails.value.status : null,
  failure.value ? (failureDetails.value.code || "REQUEST_FAILED") : "",
));
const unavailableState = computed<WorkspaceStateKind | null>(() => (
  targetState.value === "ready" ? null : targetState.value
));
const targetDescription = computed(() => {
  if (targetState.value === "invalid") return "URL 中的 selected 不是有效的公开 Incident UUID；不会推断替代目标。";
  return failureDetails.value.message;
});
const retryable = computed(() => (
  targetState.value === "error"
  || targetState.value === "permission-denied"
  || targetState.value === "expired"
));

const latestInvestigation = computed(() => [...investigations.value].sort((left, right) => (
  Date.parse(right.updated_at || right.created_at) - Date.parse(left.updated_at || left.created_at)
))[0] ?? null);
const agentConclusion = computed(() => latestInvestigation.value?.outcome
  || latestInvestigation.value?.failure_summary
  || latestInvestigation.value?.objective
  || "尚无已持久化 Agent 结论");
const approvalLabel = computed(() => {
  if (!plan.value) return "尚无 Remediation Plan";
  if (plan.value.decision) return plan.value.decision.decision === "approved" ? "已批准" : "已拒绝";
  return plan.value.status === "awaiting_approval" ? "等待 Approval" : plan.value.status;
});
const verificationLabel = computed(() => verification.value?.status || "NOT RUN");

async function load() {
  controller?.abort();
  const identity = ++requestIdentity.value;
  controller = new AbortController();
  loading.value = true;
  failure.value = null;
  incident.value = null;
  if (incidentInspectorFailureKind(props.incidentID) === "invalid") {
    loading.value = false;
    return;
  }
  try {
    const [nextIncident, evidence, investigationRows, plans, nextDelivery, verificationRows] = await Promise.all([
      getIncident(props.incidentID, controller.signal),
      listIncidentEvidence(props.incidentID, "", controller.signal),
      listIncidentInvestigations(props.incidentID, "", controller.signal),
      listIncidentRemediationPlans(props.incidentID, "", controller.signal),
      getIncidentDelivery(props.incidentID, controller.signal),
      listIncidentVerifications(props.incidentID, "", controller.signal),
    ]);
    if (identity !== requestIdentity.value || controller.signal.aborted) return;
    incident.value = nextIncident;
    evidenceCount.value = evidence.items.length;
    investigations.value = investigationRows.items;
    plan.value = plans.items[0] ?? null;
    delivery.value = nextDelivery;
    verification.value = latestVerificationRun(verificationRows.items);
  } catch (cause) {
    if (identity !== requestIdentity.value || controller.signal.aborted) return;
    failure.value = cause;
  } finally {
    if (identity === requestIdentity.value) loading.value = false;
  }
}

function retry() {
  void load();
}

watch(() => props.incidentID, (value) => {
  if (value) void load();
}, { immediate: true });

onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <div
    class="incident-inspector"
    data-testid="incident-inspector"
  >
    <WorkspaceState
      v-if="loading"
      kind="loading"
      title="正在读取 Incident 摘要"
      description="当前 Cycle、Evidence 与执行状态正在并行读取。"
    />
    <WorkspaceState
      v-else-if="unavailableState"
      :kind="unavailableState"
      :title="targetState === 'error' ? 'Incident 摘要读取失败' : undefined"
      :description="targetDescription"
      :code="failure ? failureDetails.code : ''"
      :request-i-d="failure ? failureDetails.requestID : ''"
      :trace-i-d="failure ? failureDetails.traceID : ''"
      :next-steps="failure ? failureDetails.nextSteps : []"
    >
      <template
        v-if="retryable"
        #actions
      >
        <UButton
          color="error"
          variant="outline"
          icon="i-lucide-refresh-cw"
          label="重试"
          @click="retry"
        />
      </template>
    </WorkspaceState>
    <template v-else-if="incident">
      <div class="inspector-badges">
        <UBadge
          color="error"
          variant="soft"
          :label="severityLabel(incident.severity)"
        />
        <UBadge
          color="neutral"
          variant="outline"
          :label="incidentStatusLabel(incident.status)"
        />
        <UBadge
          v-if="incident.attention.required"
          color="warning"
          variant="soft"
          label="需要 Attention"
        />
      </div>

      <section
        class="inspector-summary"
        aria-labelledby="incident-inspector-summary"
      >
        <h3 id="incident-inspector-summary">
          {{ incident.summary || "Incident Cycle 进行中" }}
        </h3>
        <p>{{ incident.operational_context.cluster }} · {{ incident.operational_context.namespace }}/{{ incident.operational_context.service }}</p>
        <code translate="no">{{ incident.id }}</code>
      </section>

      <dl class="inspector-facts">
        <div>
          <dt>Agent 结论</dt><dd>
            <Bot
              :size="14"
              aria-hidden="true"
            />{{ agentConclusion }}
          </dd>
        </div>
        <div>
          <dt>Evidence</dt><dd>
            <FileCheck2
              :size="14"
              aria-hidden="true"
            />{{ evidenceCount }} 条当前投影
          </dd>
        </div>
        <div>
          <dt>Approval</dt><dd>
            <ShieldCheck
              :size="14"
              aria-hidden="true"
            />{{ approvalLabel }}
          </dd>
        </div>
        <div>
          <dt>Delivery</dt><dd>
            <GitPullRequest
              :size="14"
              aria-hidden="true"
            />{{ delivery?.status || "NOT RUN" }}
          </dd>
        </div>
        <div>
          <dt>Verification</dt><dd>
            <ShieldCheck
              :size="14"
              aria-hidden="true"
            />{{ verificationLabel }}
          </dd>
        </div>
        <div><dt>最近更新</dt><dd>{{ formatIncidentTime(incident.updated_at) }}</dd></div>
      </dl>

      <UAlert
        color="neutral"
        variant="subtle"
        icon="i-lucide-shield-check"
        title="只读摘要"
        description="Approval、Delivery、Verification 的事故操作只在 Incident 详情页进行；此 Inspector 不发起写入。"
      />
    </template>
    <UAlert
      v-else
      color="neutral"
      variant="soft"
      icon="i-lucide-search-x"
      title="未选择 Incident"
      description="从列表选择一条 Incident 以查看当前 Cycle 摘要。"
    />
  </div>
</template>

<style scoped>
.incident-inspector { display: grid; min-width: 0; gap: var(--co-space-4); }
.inspector-badges { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.inspector-summary { display: grid; min-width: 0; gap: var(--co-space-2); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.inspector-summary h3, .inspector-summary p { margin: 0; overflow-wrap: anywhere; }
.inspector-summary h3 { color: var(--co-text-primary); font-size: 17px; line-height: 1.4; }
.inspector-summary p { color: var(--co-text-secondary); font-size: 12px; }
.inspector-summary code { color: var(--co-text-muted); overflow-wrap: anywhere; }
.inspector-facts { display: grid; gap: var(--co-space-3); margin: 0; }
.inspector-facts div { display: grid; gap: 3px; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-subtle); }
.inspector-facts dt { color: var(--co-text-muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.inspector-facts dd { display: flex; min-width: 0; align-items: flex-start; gap: var(--co-space-2); margin: 0; color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
</style>
