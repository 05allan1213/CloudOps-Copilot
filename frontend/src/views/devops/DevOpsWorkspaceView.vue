<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from "vue";
import { ElMessageBox } from "element-plus";
import {
  Activity,
  Boxes,
  CheckCircle2,
  CircleSlash2,
  Clock3,
  FileDiff,
  GitBranch,
  GitCommit,
  GitPullRequest,
  KeyRound,
  Link2,
  LockKeyhole,
  Play,
  RefreshCw,
  ShieldCheck,
  Snowflake,
  TriangleAlert,
  Workflow,
} from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import type { ActionCard, OperationPlan, OperationPlanProposalInput } from "../../api/agent";
import type { OperationExecution, ProviderBranch } from "../../api/devops";
import HashValue from "../../components/incidents/HashValue.vue";
import JSONSnapshot from "../../components/incidents/JSONSnapshot.vue";
import ResultBadge from "../../components/incidents/ResultBadge.vue";
import { useDevOpsWorkspaceStore } from "../../stores/devOpsWorkspace";

type WorkspaceView = "operations" | "identity";
type AuthoritySubject = ActionCard | OperationPlan;

const route = useRoute();
const router = useRouter();
const store = useDevOpsWorkspaceStore();
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium" });
let controller: AbortController | undefined;
let pollTimer: number | undefined;

const workspace = computed(() => store.workspace);
const plans = computed(() => workspace.value?.operation_plans ?? []);
const cards = computed(() => workspace.value?.action_cards ?? []);
const executions = computed(() => workspace.value?.executions ?? []);
const activeView = computed<WorkspaceView>(() => queryValue(route.query.view) === "identity" ? "identity" : "operations");
const selectedSubjectID = computed(() => queryValue(route.query.subject));
const selectedOperationID = computed(() => queryValue(route.query.operation));
const selectedPlan = computed(() => plans.value.find((item) => item.id === selectedSubjectID.value) ?? null);
const selectedCard = computed(() => cards.value.find((item) => item.id === selectedSubjectID.value) ?? null);
const selectedSubject = computed<AuthoritySubject | null>(() => selectedPlan.value ?? selectedCard.value);
const subjectExecution = computed(() => executions.value.find((item) => item.subject_id === selectedSubjectID.value) ?? null);
const selectedExecution = computed(() => executions.value.find((item) => item.id === selectedOperationID.value)
  ?? subjectExecution.value
  ?? executions.value[0]
  ?? null);
const selectedAuthorization = computed(() => selectedSubject.value?.authorization ?? null);
const canAuthorize = computed(() => selectedSubject.value?.status === "proposed");
const canExecute = computed(() => selectedSubject.value?.status === "authorized"
  && selectedAuthorization.value?.authorized_content_hash === selectedSubject.value.content_hash
  && !subjectExecution.value);
const selectedPayload = computed(() => {
  const subject = selectedSubject.value;
  if (!subject) return {};
  if (isOperationPlan(subject)) {
    return {
      target: subject.target,
      parameters: subject.parameters,
      intended_state: subject.intended_state,
      preconditions: subject.preconditions,
      verification_intent: subject.verification_intent,
    };
  }
  return { target: subject.target, parameters: subject.parameters, preconditions: subject.preconditions };
});
const proposedCount = computed(() => [...plans.value, ...cards.value].filter((item) => item.status === "proposed").length);
const authorizedCount = computed(() => [...plans.value, ...cards.value].filter((item) => item.status === "authorized").length);
const verifiedCount = computed(() => executions.value.filter((item) => item.verification?.status === "passed").length);
const scenarioDeployment = computed(() => store.scenarioResources?.items.find((item) =>
  item.kind === "Deployment"
  && item.namespace === "demo"
  && item.name === "cloudops-scenario-fault"
  && Boolean(item.labels["cloudops.io/scenario-id"]),
) ?? null);
const scenarioID = computed(() => scenarioDeployment.value?.labels["cloudops.io/scenario-id"] ?? "");
const scenarioInvestigation = computed(() => store.investigations.find((item) =>
  item.subject_type === "alert"
  && item.scenario_id === scenarioID.value
  && item.status === "completed"
  && item.evidence_count > 0,
) ?? null);
const scenarioFreeze = computed(() => workspace.value?.change_freezes.find((item) =>
  item.target.cluster_id === store.scenarioResources?.scope.cluster_id
  && item.target.namespace === "demo"
  && item.target.workload_name === "cloudops-scenario-fault"
  && item.target.scenario_id === scenarioID.value,
) ?? null);
const scenarioPlan = computed(() => plans.value.find((item) => {
  const target = objectValue(item.target);
  return item.operation_type === "kubernetes.deployment.scale" && target?.scenario_id === scenarioID.value;
}) ?? null);
const canProposeScenarioPlan = computed(() => Boolean(
  scenarioDeployment.value
  && scenarioID.value
  && scenarioDeployment.value.resource_version
  && scenarioDeployment.value.workload?.desired_replicas === 1
  && scenarioInvestigation.value
  && !scenarioFreeze.value?.enabled
  && !scenarioPlan.value
  && !store.scenarioPlanningError,
));
const scenarioProposalBlocker = computed(() => {
  if (store.scenarioPlanningError) return store.scenarioPlanningError;
  if (!scenarioDeployment.value || !scenarioID.value) return "Scenario Deployment 不可用；先运行 make scenario-up。";
  if (scenarioDeployment.value.workload?.desired_replicas === 0) return "Scenario fault 已恢复到 0 replicas，无需重复创建 Plan。";
  if (!scenarioDeployment.value.resource_version) return "当前 Kubernetes projection 缺少 resourceVersion，拒绝创建弱 precondition Plan。";
  if (!scenarioInvestigation.value) return "尚无同一 Scenario 的已完成 Agent Investigation 与 Evidence。";
  if (scenarioFreeze.value?.enabled) return "当前 target 已进入 change freeze，不能创建 recovery Plan。";
  if (scenarioPlan.value) return `该 Scenario 已有 ${scenarioPlan.value.status} Operation Plan。`;
  return "";
});

function queryValue(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw : "";
}

function isOperationPlan(subject: AuthoritySubject): subject is OperationPlan {
  return subject.authority === "high_impact";
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function subjectType(subject: AuthoritySubject): string {
  return isOperationPlan(subject) ? subject.operation_type : subject.action_type;
}

function formatTime(value?: string): string {
  if (!value) return "未记录";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间无效" : dateFormatter.format(date);
}

function shortIdentity(value?: string, size = 8): string {
  if (!value) return "未记录";
  if (value.length <= size * 2 + 1) return value;
  return `${value.slice(0, size)}…${value.slice(-size)}`;
}

function providerOutcome(provider: ProviderBranch): "PASS" | "FAIL" | "NOT RUN" {
  if (provider.state === "available") return "PASS";
  if (!provider.enabled || provider.state === "disabled" || provider.state === "not_configured") return "NOT RUN";
  return "FAIL";
}

function ensureSelection() {
  if (!workspace.value || selectedSubject.value) return;
  const preferred = [...plans.value, ...cards.value].find((item) => item.status === "proposed")
    ?? [...plans.value, ...cards.value].find((item) => item.status === "authorized")
    ?? plans.value[0]
    ?? cards.value[0];
  if (preferred) void router.replace({ query: { ...route.query, subject: preferred.id } });
}

function setView(view: WorkspaceView) {
  void router.replace({ query: { ...route.query, view: view === "operations" ? undefined : view } });
}

function selectSubject(subject: AuthoritySubject) {
  const execution = executions.value.find((item) => item.subject_id === subject.id);
  void router.replace({
    query: { ...route.query, subject: subject.id, operation: execution?.id },
  });
}

function selectExecution(execution: OperationExecution) {
  void router.replace({
    query: { ...route.query, view: undefined, subject: execution.subject_id, operation: execution.id },
  });
}

async function refresh() {
  store.clearFeedback();
  await store.load(true);
  ensureSelection();
}

async function authorizeSelected() {
  const subject = selectedSubject.value;
  if (!subject || !canAuthorize.value) return;
  try {
    const answer = await ElMessageBox.prompt(
      `Content hash: ${subject.content_hash}`,
      isOperationPlan(subject) ? "Owner review · immutable Operation Plan" : "Owner review · exact Action Card",
      {
        confirmButtonText: "授权 exact hash",
        cancelButtonText: "取消",
        inputType: "textarea",
        inputPlaceholder: "记录本次 Owner review 的理由",
        inputValidator: (value) => {
          const reason = value.trim();
          if (reason.length < 2) return "请填写授权理由。";
          if (reason.length > 1024) return "授权理由不能超过 1024 个字符。";
          return true;
        },
      },
    );
    if (isOperationPlan(subject)) await store.authorizePlan(subject, answer.value.trim());
    else await store.authorizeCard(subject, answer.value.trim());
  } catch {
    // Closing the Owner review dialog is not an operation failure.
  }
}

async function executeSelected() {
  const subject = selectedSubject.value;
  if (!subject || !canExecute.value) return;
  try {
    await ElMessageBox.confirm(
      `仅执行已授权 content hash ${subject.content_hash}。`,
      isOperationPlan(subject) ? "执行 high-impact Operation Plan" : "执行 local reversible action",
      { confirmButtonText: "排队执行", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  const execution = isOperationPlan(subject)
    ? await store.executePlan(subject)
    : await store.executeCard(subject);
  if (execution) {
    await router.replace({ query: { ...route.query, subject: subject.id, operation: execution.id } });
  }
}

async function proposeScenarioRecovery() {
  const deployment = scenarioDeployment.value;
  const run = scenarioInvestigation.value;
  const resourceVersion = deployment?.resource_version;
  if (!canProposeScenarioPlan.value || !deployment || !run || !resourceVersion || !scenarioID.value || !store.scenarioResources) return;
  try {
    await ElMessageBox.confirm(
      `Target: demo/cloudops-scenario-fault\nScenario: ${scenarioID.value}\nresourceVersion: ${resourceVersion}\n\n创建操作不会授权或执行 Kubernetes mutation。`,
      "基于 Agent Evidence 创建 immutable Recovery Plan",
      { confirmButtonText: "创建 exact Plan", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  const input: OperationPlanProposalInput = {
    run_id: run.id,
    action_type: "kubernetes.deployment.scale",
    target: {
      cluster_id: store.scenarioResources.scope.cluster_id,
      environment: store.scenarioResources.scope.environment,
      namespace: "demo",
      workload_kind: "Deployment",
      workload_name: "cloudops-scenario-fault",
      scenario_id: scenarioID.value,
    },
    parameters: { replicas: 0 },
    intended_state: { replicas: 0 },
    preconditions: [
      { type: "deployment.replicas", expected_replicas: 1 },
      { type: "deployment.resource_version", expected_resource_version: resourceVersion },
      { type: "local.change_freeze", expected_enabled: false, expected_version: scenarioFreeze.value?.row_version ?? 0 },
    ],
    risk: "Scale only the bounded failing Scenario Deployment to 0 replicas; healthy traffic and retained CloudOps history remain.",
    verification_intent: { type: "kubernetes.deployment.scale", expected_replicas: 0 },
    expires_at: new Date(Date.now() + 15 * 60 * 1000).toISOString(),
  };
  const plan = await store.proposeScenarioPlan(input);
  if (plan) await router.replace({ query: { ...route.query, view: undefined, subject: plan.id, operation: undefined } });
}

watch(() => workspace.value?.collected_at, ensureSelection);

onMounted(async () => {
  controller = new AbortController();
  await store.load(false, controller.signal);
  ensureSelection();
  pollTimer = window.setInterval(() => {
    if (store.activeExecutions.length && !store.loading && !store.mutatingSubjectID) void store.load(true);
  }, 1500);
});

onBeforeUnmount(() => {
  controller?.abort();
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
});
</script>

<template>
  <section class="devops-workspace" aria-labelledby="devops-heading">
    <div class="workspace-scroll">
      <header class="workspace-header">
        <div class="title-block">
          <span class="title-mark"><Workflow :size="20" aria-hidden="true" /></span>
          <div><span class="section-kicker">Controlled operations</span><h1 id="devops-heading">DevOps Workspace</h1></div>
        </div>
        <dl class="workspace-stats">
          <div><dt>Review</dt><dd>{{ proposedCount }}</dd></div>
          <div><dt>Authorized</dt><dd>{{ authorizedCount }}</dd></div>
          <div><dt>Executing</dt><dd>{{ store.activeExecutions.length }}</dd></div>
          <div><dt>Verified</dt><dd>{{ verifiedCount }}</dd></div>
        </dl>
        <button type="button" class="icon-button" :disabled="store.loading" aria-label="刷新 DevOps Workspace" title="刷新" @click="refresh">
          <RefreshCw :size="18" :class="{ spinning: store.loading }" aria-hidden="true" />
        </button>
      </header>

      <div v-if="store.error" class="feedback-strip is-error" role="alert">
        <CircleSlash2 :size="17" aria-hidden="true" /><span>{{ store.error }}</span><button type="button" @click="refresh">重试</button>
      </div>
      <div v-else-if="store.notice" class="feedback-strip is-success" role="status" aria-live="polite">
        <CheckCircle2 :size="17" aria-hidden="true" /><span>{{ store.notice }}</span>
      </div>

      <section v-if="workspace" class="provider-band" aria-labelledby="provider-branch-heading">
        <header><h2 id="provider-branch-heading">Provider branches</h2><time :datetime="workspace.collected_at">{{ formatTime(workspace.collected_at) }}</time></header>
        <ul>
          <li v-for="provider in workspace.providers" :key="provider.provider" class="provider-item">
            <span class="provider-icon"><Boxes v-if="provider.provider === 'kubernetes'" :size="17" aria-hidden="true" /><GitPullRequest v-else :size="17" aria-hidden="true" /></span>
            <div><strong>{{ provider.provider }}</strong><small>{{ provider.role }}</small></div>
            <ResultBadge :result="providerOutcome(provider)" :label="providerOutcome(provider)" />
            <p>{{ provider.detail }}</p>
          </li>
        </ul>
      </section>

      <nav class="workspace-tabs" role="tablist" aria-label="DevOps Workspace 视图">
        <button type="button" role="tab" :aria-selected="activeView === 'operations'" @click="setView('operations')"><ShieldCheck :size="16" aria-hidden="true" />Operations</button>
        <button type="button" role="tab" :aria-selected="activeView === 'identity'" @click="setView('identity')"><GitCommit :size="16" aria-hidden="true" />Delivery identity</button>
      </nav>

      <div v-if="!workspace && store.loading" class="workspace-state" role="status"><RefreshCw :size="22" class="spinning" aria-hidden="true" />正在读取 durable operations…</div>
      <div v-else-if="!workspace" class="workspace-state"><TriangleAlert :size="22" aria-hidden="true" />当前没有可用 DevOps projection。</div>

      <main v-else-if="activeView === 'operations'" class="operations-layout">
        <aside class="authority-queue" aria-label="Authority review queue">
          <section class="scenario-plan-builder" aria-labelledby="scenario-plan-heading">
            <header><TriangleAlert :size="15" aria-hidden="true" /><h2 id="scenario-plan-heading">Scenario Recovery</h2><span>{{ scenarioID ? "ACTIVE" : "NOT RUN" }}</span></header>
            <dl v-if="scenarioDeployment" class="scenario-plan-facts">
              <div><dt>Target</dt><dd><code translate="no">demo/cloudops-scenario-fault</code></dd></div>
              <div><dt>Scenario</dt><dd><code translate="no">{{ scenarioID }}</code></dd></div>
              <div><dt>Replicas</dt><dd>{{ scenarioDeployment.workload?.ready_replicas ?? 0 }} / {{ scenarioDeployment.workload?.desired_replicas ?? 0 }} ready</dd></div>
              <div><dt>Agent Evidence</dt><dd>{{ scenarioInvestigation ? `${scenarioInvestigation.evidence_count} citations` : "NOT RUN" }}</dd></div>
            </dl>
            <p v-if="scenarioProposalBlocker" class="scenario-plan-note">{{ scenarioProposalBlocker }}</p>
            <button type="button" class="scenario-plan-command" :disabled="!canProposeScenarioPlan || Boolean(store.mutatingSubjectID)" @click="proposeScenarioRecovery">
              <Workflow :size="15" aria-hidden="true" />创建 exact Recovery Plan
            </button>
          </section>
          <section>
            <header><LockKeyhole :size="15" aria-hidden="true" /><h2>Operation Plans</h2><span>{{ plans.length }}</span></header>
            <p v-if="!plans.length" class="empty-row">无持久化 Plan。</p>
            <button v-for="plan in plans" :key="plan.id" type="button" class="queue-item" :class="{ selected: plan.id === selectedSubjectID }" @click="selectSubject(plan)">
              <span class="queue-main"><strong>{{ plan.operation_type }}</strong><small>{{ shortIdentity(plan.id) }}</small></span>
              <ResultBadge :result="plan.status" />
              <code translate="no">{{ shortIdentity(plan.content_hash, 10) }}</code>
            </button>
          </section>
          <section>
            <header><KeyRound :size="15" aria-hidden="true" /><h2>Action Cards</h2><span>{{ cards.length }}</span></header>
            <p v-if="!cards.length" class="empty-row">无持久化 Action Card。</p>
            <button v-for="card in cards" :key="card.id" type="button" class="queue-item" :class="{ selected: card.id === selectedSubjectID }" @click="selectSubject(card)">
              <span class="queue-main"><strong>{{ card.action_type }}</strong><small>{{ shortIdentity(card.id) }}</small></span>
              <ResultBadge :result="card.status" />
              <code translate="no">{{ shortIdentity(card.content_hash, 10) }}</code>
            </button>
          </section>
          <section>
            <header><Snowflake :size="15" aria-hidden="true" /><h2>Change freezes</h2><span>{{ workspace.change_freezes.length }}</span></header>
            <p v-if="!workspace.change_freezes.length" class="empty-row">无本地 freeze 记录。</p>
            <div v-for="freeze in workspace.change_freezes" :key="`${freeze.target.cluster_id}/${freeze.target.namespace}/${freeze.target.workload_name}`" class="freeze-row">
              <div><strong>{{ freeze.target.namespace }}/{{ freeze.target.workload_name }}</strong><small>row v{{ freeze.row_version }}</small></div>
              <ResultBadge :result="freeze.enabled ? 'warning' : 'success'" :label="freeze.enabled ? 'FROZEN' : 'OPEN'" />
              <p>{{ freeze.reason }}</p>
            </div>
          </section>
        </aside>

        <div class="operation-detail">
          <section v-if="!selectedSubject" class="detail-empty"><LockKeyhole :size="24" aria-hidden="true" /><h2>没有可审查的 authority subject</h2></section>
          <template v-else>
            <header class="subject-header">
              <div><span>{{ isOperationPlan(selectedSubject) ? "High impact · immutable Plan" : "Local · reversible" }}</span><h2>{{ subjectType(selectedSubject) }}</h2><code translate="no">{{ selectedSubject.id }}</code></div>
              <ResultBadge :result="selectedSubject.status" />
            </header>

            <section class="detail-section authority-contract" aria-labelledby="authority-contract-heading">
              <header><div><ShieldCheck :size="17" aria-hidden="true" /><h3 id="authority-contract-heading">Owner review & exact authorization</h3></div><span>{{ selectedAuthorization ? "BOUND" : "NOT AUTHORIZED" }}</span></header>
              <HashValue label="Content hash" :value="selectedSubject.content_hash" />
              <HashValue v-if="selectedAuthorization" label="Authorized hash" :value="selectedAuthorization.authorized_content_hash" />
              <dl class="contract-facts">
                <div><dt>Configuration Revision</dt><dd><code translate="no">{{ isOperationPlan(selectedSubject) ? selectedSubject.configuration_revision_id : "run-bound" }}</code></dd></div>
                <div><dt>Expires</dt><dd>{{ formatTime(selectedSubject.expires_at) }}</dd></div>
                <div><dt>Risk</dt><dd>{{ selectedSubject.risk }}</dd></div>
                <div v-if="selectedAuthorization"><dt>Authorized by</dt><dd>{{ selectedAuthorization.authorized_by }} · {{ selectedAuthorization.reason }}</dd></div>
              </dl>
              <JSONSnapshot title="Exact material payload" :value="selectedPayload" />
              <div class="subject-actions">
                <button v-if="canAuthorize" type="button" class="primary-command" :disabled="Boolean(store.mutatingSubjectID)" @click="authorizeSelected"><KeyRound :size="16" aria-hidden="true" />授权 exact hash</button>
                <button v-if="canExecute" type="button" class="danger-command" :disabled="Boolean(store.mutatingSubjectID)" @click="executeSelected"><Play :size="16" aria-hidden="true" />排队执行</button>
                <span v-if="subjectExecution" class="execution-bound"><Link2 :size="15" aria-hidden="true" />Execution {{ shortIdentity(subjectExecution.id) }}</span>
              </div>
            </section>

            <section class="detail-section execution-index" aria-labelledby="execution-index-heading">
              <header><div><Activity :size="17" aria-hidden="true" /><h3 id="execution-index-heading">Execution & audit</h3></div><span>{{ executions.length }}</span></header>
              <div v-if="!executions.length" class="empty-row">尚无 execution；零授权状态没有外部写。</div>
              <div v-else class="execution-rail" role="list">
                <button v-for="item in executions" :key="item.id" type="button" :class="{ selected: item.id === selectedExecution?.id }" role="listitem" @click="selectExecution(item)">
                  <span><strong>{{ item.operation_type }}</strong><small>{{ formatTime(item.created_at) }}</small></span><ResultBadge :result="item.status" /><code>{{ shortIdentity(item.id) }}</code>
                </button>
              </div>
            </section>

            <template v-if="selectedExecution">
              <section class="detail-section execution-summary">
                <header><div><Clock3 :size="17" aria-hidden="true" /><h3>Execution {{ shortIdentity(selectedExecution.id) }}</h3></div><ResultBadge :result="selectedExecution.status" /></header>
                <dl class="contract-facts compact">
                  <div><dt>Attempt</dt><dd>{{ selectedExecution.attempt }}</dd></div>
                  <div><dt>Configuration</dt><dd><code>{{ selectedExecution.configuration_revision_id }}</code></dd></div>
                  <div><dt>Effect boundary</dt><dd>{{ formatTime(selectedExecution.external_effect_started_at) }}</dd></div>
                  <div><dt>Completed</dt><dd>{{ formatTime(selectedExecution.completed_at) }}</dd></div>
                  <div v-if="selectedExecution.failure_code"><dt>Failure</dt><dd>{{ selectedExecution.failure_code }} · {{ selectedExecution.failure_summary }}</dd></div>
                </dl>
                <nav v-if="selectedExecution.links.length" class="verification-links" aria-label="Execution context links">
                  <RouterLink v-for="link in selectedExecution.links" :key="`${link.kind}:${link.href}`" :to="link.href"><Link2 :size="15" aria-hidden="true" />{{ link.label }}<span v-if="link.status">{{ link.status }}</span></RouterLink>
                </nav>
                <ol class="audit-timeline">
                  <li v-for="event in selectedExecution.events" :key="event.id">
                    <span>{{ event.sequence }}</span><div><header><strong>{{ event.type }}</strong><time :datetime="event.occurred_at">{{ formatTime(event.occurred_at) }}</time></header><code>{{ shortIdentity(event.content_hash, 10) }}</code><JSONSnapshot title="Audit payload" :value="event.payload" /></div>
                  </li>
                </ol>
              </section>

              <section id="verification" class="detail-section verification-section" aria-labelledby="verification-heading">
                <header><div><CheckCircle2 :size="17" aria-hidden="true" /><h3 id="verification-heading">Current Evidence Verify</h3></div><ResultBadge :result="selectedExecution.verification?.status ?? 'pending'" /></header>
                <div v-if="!selectedExecution.verification" class="empty-row">等待当前 post-effect observation。</div>
                <template v-else>
                  <p>{{ selectedExecution.verification.summary }}</p>
                  <dl class="contract-facts compact"><div><dt>Source</dt><dd>{{ selectedExecution.verification.source }}</dd></div><div><dt>Observed</dt><dd>{{ formatTime(selectedExecution.verification.observed_at) }}</dd></div></dl>
                  <HashValue label="Evidence hash" :value="selectedExecution.verification.content_hash" />
                  <JSONSnapshot title="Provider identity" :value="selectedExecution.verification.provider_identity" />
                  <JSONSnapshot title="Current Evidence" :value="selectedExecution.verification.evidence" />
                </template>
              </section>
            </template>
          </template>
        </div>
      </main>

      <main v-else class="identity-view">
        <section class="identity-section" aria-labelledby="candidate-heading">
          <header><div><FileDiff :size="17" aria-hidden="true" /><h2 id="candidate-heading">ChangeCandidate</h2></div><span>{{ workspace.change_candidates.length }}</span></header>
          <div class="table-scroll" tabindex="0" role="region" aria-label="ChangeCandidate table">
            <table><thead><tr><th>Change</th><th>Source identity</th><th>GitOps / image</th><th>Evidence hash</th><th>Observed</th></tr></thead><tbody>
              <tr v-if="!workspace.change_candidates.length"><td colspan="5">无持久化 ChangeCandidate。</td></tr>
              <tr v-for="item in workspace.change_candidates" :key="item.id"><td><strong>{{ item.category }}</strong><code>{{ shortIdentity(item.id) }}</code></td><td><span>{{ item.repository || item.source_type }}</span><code>{{ shortIdentity(item.commit_sha, 10) }}</code></td><td><code>{{ shortIdentity(item.gitops_revision, 10) }}</code><code>{{ shortIdentity(item.image_digest, 10) }}</code></td><td><code>{{ shortIdentity(item.content_hash, 10) }}</code></td><td>{{ formatTime(item.change_time) }}</td></tr>
            </tbody></table>
          </div>
        </section>

        <section class="identity-section" aria-labelledby="baseline-heading">
          <header><div><GitCommit :size="17" aria-hidden="true" /><h2 id="baseline-heading">DeploymentBaseline</h2></div><span>{{ workspace.deployment_baselines.length }}</span></header>
          <div class="table-scroll" tabindex="0" role="region" aria-label="DeploymentBaseline table">
            <table><thead><tr><th>Target</th><th>Source revision</th><th>Image digest</th><th>GitOps revision</th><th>Verification</th></tr></thead><tbody>
              <tr v-if="!workspace.deployment_baselines.length"><td colspan="5">无 verified DeploymentBaseline。</td></tr>
              <tr v-for="item in workspace.deployment_baselines" :key="item.id"><td><strong>{{ item.namespace }}/{{ item.workload_name }}</strong><small>{{ item.cluster }} · {{ item.environment }} · {{ item.container_name }}</small></td><td><code>{{ shortIdentity(item.source_revision, 10) }}</code></td><td><code>{{ shortIdentity(item.image_digest, 10) }}</code></td><td><code>{{ shortIdentity(item.gitops_revision, 10) }}</code></td><td><ResultBadge :result="item.status" /><small>{{ formatTime(item.verified_at) }}</small></td></tr>
            </tbody></table>
          </div>
        </section>

        <section class="identity-section" aria-labelledby="delivery-heading">
          <header><div><GitBranch :size="17" aria-hidden="true" /><h2 id="delivery-heading">PR / CI / Argo / rollout</h2></div><span>{{ workspace.deliveries.length }}</span></header>
          <div class="table-scroll" tabindex="0" role="region" aria-label="Delivery projection table">
            <table><thead><tr><th>Repository / PR</th><th>Exact commit</th><th>CI</th><th>Argo exact revision</th><th>Rollout</th></tr></thead><tbody>
              <tr v-if="!workspace.deliveries.length"><td colspan="5">GitHub/Argo delivery branch NOT RUN。</td></tr>
              <tr v-for="item in workspace.deliveries" :key="item.id"><td><strong>{{ item.repository }}</strong><span>PR #{{ item.pull_request_number || "—" }} · {{ item.pull_request_state || "NOT RUN" }}</span></td><td><code>{{ shortIdentity(item.commit_sha, 10) }}</code><code>{{ shortIdentity(item.merged_commit_sha, 10) }}</code></td><td><ResultBadge :result="item.ci_status || 'not_run'" :label="item.ci_status || 'NOT RUN'" /></td><td><code>{{ shortIdentity(item.target_revision, 10) }}</code><span>{{ item.argo_sync_status || "NOT RUN" }} / {{ item.argo_health_status || "NOT RUN" }}</span></td><td><ResultBadge :result="item.status" /><span>{{ item.available_replicas }} / {{ item.desired_replicas }} available</span></td></tr>
            </tbody></table>
          </div>
        </section>
      </main>
    </div>
  </section>
</template>

<style scoped>
.devops-workspace { min-width: 0; background: var(--co-bg-canvas); }
.workspace-scroll { min-width: 0; }
.workspace-header { display: grid; min-height: 76px; grid-template-columns: minmax(240px, 1fr) auto 40px; align-items: center; gap: var(--co-space-5); padding: 0 var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.title-block, .title-block > div, .provider-band > header, .provider-item, .provider-item > div, .authority-queue section > header, .subject-header, .detail-section > header, .detail-section > header > div, .identity-section > header, .identity-section > header > div { display: flex; min-width: 0; align-items: center; }
.title-block { gap: var(--co-space-3); }
.title-block > div { display: block; }
.title-mark { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-panel); color: var(--co-action-primary); background: var(--co-status-info-bg); }
.section-kicker { color: var(--co-text-muted); font-size: 10px; font-weight: 800; text-transform: uppercase; }
.workspace-header h1 { margin: 1px 0 0; font-size: 19px; }
.workspace-stats { display: flex; gap: var(--co-space-5); margin: 0; }
.workspace-stats div { min-width: 62px; }
.workspace-stats dt { color: var(--co-text-muted); font-size: 9px; text-transform: uppercase; }
.workspace-stats dd { margin: 1px 0 0; font-size: 15px; font-weight: 800; font-variant-numeric: tabular-nums; }
.icon-button { display: grid; width: 40px; height: 40px; place-items: center; padding: 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); cursor: pointer; }
.icon-button:hover { border-color: var(--co-border-strong); color: var(--co-action-primary); background: var(--co-bg-hover); }
.icon-button:disabled { cursor: wait; opacity: .6; }
.feedback-strip { display: grid; min-height: 44px; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-5); border-bottom: 1px solid; font-size: 11px; }
.feedback-strip.is-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.feedback-strip.is-success { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.feedback-strip button { min-height: 30px; padding: 0 var(--co-space-3); border: 1px solid currentColor; border-radius: var(--co-radius-control); color: inherit; background: transparent; cursor: pointer; }
.provider-band { padding: var(--co-space-4) var(--co-space-5); border-bottom: 1px solid var(--co-border-default); }
.provider-band > header { justify-content: space-between; margin-bottom: var(--co-space-2); }
.provider-band h2 { margin: 0; font-size: 11px; text-transform: uppercase; }
.provider-band time { color: var(--co-text-muted); font-size: 9px; }
.provider-band ul { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; padding: 0; list-style: none; }
.provider-item { display: grid; min-height: 66px; grid-template-columns: 30px minmax(0, 1fr) auto; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.provider-icon { display: grid; width: 30px; height: 30px; place-items: center; border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-subtle); }
.provider-item > div { display: grid; align-content: center; }
.provider-item strong { font-size: 11px; text-transform: capitalize; }
.provider-item small { color: var(--co-text-muted); font-size: 8px; text-transform: uppercase; }
.provider-item p { grid-column: 2 / -1; min-width: 0; margin: -4px 0 0; color: var(--co-text-muted); font-size: 9px; overflow-wrap: anywhere; }
.workspace-tabs { position: sticky; top: var(--co-header-height); z-index: var(--co-z-sticky); display: flex; min-height: 46px; padding: 0 var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.workspace-tabs button { display: inline-flex; min-width: 150px; align-items: center; justify-content: center; gap: var(--co-space-2); padding: 0 var(--co-space-4); border: 0; border-bottom: 2px solid transparent; color: var(--co-text-secondary); background: transparent; cursor: pointer; font-size: 11px; font-weight: 750; }
.workspace-tabs button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.workspace-tabs button[aria-selected="true"] { border-bottom-color: var(--co-action-primary); color: var(--co-action-primary); }
.workspace-state, .detail-empty { display: grid; min-height: 300px; place-content: center; justify-items: center; gap: var(--co-space-3); color: var(--co-text-muted); }
.operations-layout { display: grid; width: min(100%, var(--co-content-max-width)); min-height: 720px; grid-template-columns: minmax(270px, 340px) minmax(0, 1fr); margin: 0 auto; }
.authority-queue { min-width: 0; border-right: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.authority-queue section { padding: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.authority-queue section > header { gap: var(--co-space-2); margin-bottom: var(--co-space-2); }
.authority-queue h2 { flex: 1; margin: 0; font-size: 11px; }
.authority-queue header > span { color: var(--co-text-muted); font-size: 9px; }
.scenario-plan-builder { background: var(--co-status-warning-bg); }
.scenario-plan-facts { display: grid; gap: var(--co-space-2); margin: 0; }
.scenario-plan-facts div { display: grid; min-width: 0; grid-template-columns: 72px minmax(0, 1fr); gap: var(--co-space-2); }
.scenario-plan-facts dt { color: var(--co-text-muted); font-size: 8px; text-transform: uppercase; }
.scenario-plan-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-size: 9px; overflow-wrap: anywhere; }
.scenario-plan-facts code { font-size: 8px; }
.scenario-plan-note { margin: var(--co-space-3) 0; color: var(--co-status-warning-fg); font-size: 9px; line-height: 1.5; overflow-wrap: anywhere; }
.scenario-plan-command { display: inline-flex; width: 100%; min-height: 44px; align-items: center; justify-content: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-status-warning-border); border-radius: var(--co-radius-control); color: var(--co-status-warning-fg); background: var(--co-bg-surface); cursor: pointer; font-size: 10px; font-weight: 800; }
.scenario-plan-command:hover:not(:disabled) { border-color: var(--co-action-primary); color: var(--co-action-primary); background: var(--co-bg-hover); }
.scenario-plan-command:disabled { cursor: not-allowed; opacity: .55; }
.scenario-plan-command:focus-visible { outline: 2px solid var(--co-focus-ring); outline-offset: 2px; }
.queue-item { display: grid; width: 100%; min-height: 66px; grid-template-columns: minmax(0, 1fr) auto; gap: 4px var(--co-space-2); margin-top: 4px; padding: var(--co-space-2); border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-primary); background: transparent; cursor: pointer; text-align: left; }
.queue-item:hover { border-color: var(--co-border-default); background: var(--co-bg-hover); }
.queue-item.selected { border-color: var(--co-status-info-border); background: var(--co-bg-active); }
.queue-main { display: grid; min-width: 0; }
.queue-main strong { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.queue-main small, .queue-item > code { color: var(--co-text-muted); font-size: 8px; }
.queue-item > code { grid-column: 1 / -1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty-row { margin: 0; padding: var(--co-space-4); color: var(--co-text-muted); font-size: 10px; text-align: center; }
.freeze-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 4px var(--co-space-2); padding: var(--co-space-2) 0; border-top: 1px solid var(--co-border-default); }
.freeze-row > div { display: grid; min-width: 0; }
.freeze-row strong { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.freeze-row small, .freeze-row p { color: var(--co-text-muted); font-size: 8px; }
.freeze-row p { grid-column: 1 / -1; margin: 0; overflow-wrap: anywhere; }
.operation-detail { min-width: 0; background: var(--co-bg-canvas); }
.subject-header { min-height: 78px; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-4) var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.subject-header > div { display: grid; min-width: 0; gap: 1px; }
.subject-header span { color: var(--co-text-muted); font-size: 9px; text-transform: uppercase; }
.subject-header h2 { margin: 0; font-size: 16px; overflow-wrap: anywhere; }
.subject-header code { color: var(--co-text-muted); font-size: 8px; overflow-wrap: anywhere; }
.detail-section { padding: var(--co-space-5); border-bottom: 1px solid var(--co-border-default); }
.detail-section > header { min-height: 30px; justify-content: space-between; gap: var(--co-space-3); margin-bottom: var(--co-space-3); }
.detail-section > header > div, .identity-section > header > div { gap: var(--co-space-2); }
.detail-section h3 { margin: 0; font-size: 12px; }
.detail-section > header > span { color: var(--co-text-muted); font-size: 9px; font-weight: 800; }
.contract-facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: var(--co-space-3) 0; border-top: 1px solid var(--co-border-default); }
.contract-facts div { min-width: 0; padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-default); }
.contract-facts div:nth-child(odd) { padding-right: var(--co-space-4); }
.contract-facts dt { color: var(--co-text-muted); font-size: 9px; }
.contract-facts dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); font-size: 10px; overflow-wrap: anywhere; }
.contract-facts code { font-size: 9px; }
.contract-facts.compact { margin-bottom: 0; }
.subject-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); padding-top: var(--co-space-3); }
.primary-command, .danger-command { display: inline-flex; min-height: 40px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-4); border: 1px solid; border-radius: var(--co-radius-control); cursor: pointer; font-size: 11px; font-weight: 800; }
.primary-command { border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.danger-command { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.primary-command:disabled, .danger-command:disabled { cursor: wait; opacity: .55; }
.execution-bound { display: inline-flex; align-items: center; gap: 5px; color: var(--co-text-muted); font-size: 9px; }
.execution-rail { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: var(--co-space-2); }
.execution-rail button { display: grid; min-height: 60px; grid-template-columns: minmax(0, 1fr) auto; gap: 3px var(--co-space-2); padding: var(--co-space-2); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); cursor: pointer; text-align: left; }
.execution-rail button.selected { border-color: var(--co-status-info-border); background: var(--co-bg-active); }
.execution-rail button > span { display: grid; min-width: 0; }
.execution-rail strong { overflow: hidden; font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.execution-rail small, .execution-rail code { color: var(--co-text-muted); font-size: 8px; }
.execution-rail code { grid-column: 1 / -1; }
.verification-links { display: flex; flex-wrap: wrap; gap: var(--co-space-2); margin-top: var(--co-space-3); }
.verification-links a { display: inline-flex; min-height: 44px; align-items: center; gap: 5px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); font-size: 9px; font-weight: 700; }
.verification-links a span { padding-left: var(--co-space-2); color: var(--co-text-muted); }
.audit-timeline { display: grid; gap: 0; margin: var(--co-space-4) 0 0; padding: 0; list-style: none; }
.audit-timeline > li { display: grid; grid-template-columns: 28px minmax(0, 1fr); gap: var(--co-space-3); }
.audit-timeline > li > span { display: grid; width: 24px; height: 24px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); color: var(--co-text-muted); background: var(--co-bg-surface); font-size: 8px; }
.audit-timeline > li > div { min-width: 0; padding: 1px 0 var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.audit-timeline header { display: flex; justify-content: space-between; gap: var(--co-space-3); }
.audit-timeline strong { font-size: 10px; }
.audit-timeline time, .audit-timeline code { color: var(--co-text-muted); font-size: 8px; }
.verification-section > p { margin: 0; color: var(--co-text-secondary); font-size: 11px; }
.identity-view { width: min(100%, var(--co-content-max-width)); margin: 0 auto; background: var(--co-bg-surface); }
.identity-section { padding: var(--co-space-5); border-bottom: 1px solid var(--co-border-default); }
.identity-section > header { justify-content: space-between; gap: var(--co-space-3); margin-bottom: var(--co-space-3); }
.identity-section h2 { margin: 0; font-size: 13px; }
.identity-section header > span { color: var(--co-text-muted); font-size: 9px; }
.table-scroll { max-width: 100%; overflow-x: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
table { width: 100%; min-width: 860px; border-collapse: collapse; table-layout: fixed; }
th, td { padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); vertical-align: top; text-align: left; }
th { color: var(--co-text-muted); background: var(--co-bg-subtle); font-size: 9px; text-transform: uppercase; }
td { color: var(--co-text-secondary); font-size: 10px; overflow-wrap: anywhere; }
td strong, td small, td span, td code { display: block; min-width: 0; }
td strong { color: var(--co-text-primary); }
td small, td span { margin-top: 3px; color: var(--co-text-muted); font-size: 8px; }
td code { margin-top: 3px; font-size: 8px; overflow-wrap: anywhere; }
tbody tr:last-child td { border-bottom: 0; }
.spinning { animation: spin 900ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) { .workspace-stats div:nth-child(-n+2) { display: none; } .operations-layout { grid-template-columns: 280px minmax(0, 1fr); } }
@media (max-width: 820px) {
  .workspace-header { grid-template-columns: minmax(0, 1fr) 40px; padding-inline: var(--co-space-4); }
  .workspace-stats { display: none; }
  .provider-band { padding-inline: var(--co-space-4); }
  .provider-band ul { grid-template-columns: 1fr; }
  .operations-layout { grid-template-columns: 1fr; }
  .authority-queue { border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .authority-queue { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .authority-queue section:last-child { grid-column: 1 / -1; }
}
@media (max-width: 560px) {
  .workspace-header { min-height: 64px; }
  .title-mark { width: 34px; height: 34px; flex-basis: 34px; }
  .workspace-header h1 { font-size: 15px; }
  .workspace-tabs { padding: 0; }
  .workspace-tabs button { min-width: 0; flex: 1; padding-inline: var(--co-space-2); font-size: 10px; }
  .authority-queue { display: block; }
  .subject-header, .detail-section, .identity-section { padding: var(--co-space-4); }
  .contract-facts { grid-template-columns: 1fr; }
  .contract-facts div:nth-child(odd) { padding-right: 0; }
  .feedback-strip { padding-inline: var(--co-space-3); }
}
</style>
