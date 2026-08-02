<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { ActionCard, OperationPlan, OperationPlanProposalInput } from "../../api/agent";
import type {
  ChangeCandidate,
  DeploymentBaseline,
  DeliveryProjection,
  OperationExecution,
  ProviderBranch,
} from "../../api/devops";
import HashValue from "../../components/incidents/HashValue.vue";
import IncidentCommandConfirmation from "../../components/incidents/IncidentCommandConfirmation.vue";
import JSONSnapshot from "../../components/incidents/JSONSnapshot.vue";
import ResultBadge from "../../components/incidents/ResultBadge.vue";
import CopyFeedbackButton from "../../components/workspace/CopyFeedbackButton.vue";
import WorkspaceDenseList, { type DenseListSeverity } from "../../components/workspace/WorkspaceDenseList.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector from "../../components/workspace/WorkspaceInspector.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import WorkspaceStatusRow from "../../components/workspace/WorkspaceStatusRow.vue";
import WorkspaceTechnicalDetails, { type TechnicalDetailField } from "../../components/workspace/WorkspaceTechnicalDetails.vue";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { safeExternalURL } from "../../models/workbench";
import {
  classifyDevOpsRun,
  classifyDevOpsSubject,
  incidentStageHref,
  type DevOpsSubjectOwnership,
  useDevOpsWorkspaceStore,
} from "../../stores/devOpsWorkspace";

type WorkspaceView = "operations" | "identity";
type AuthoritySubject = ActionCard | OperationPlan;
type ConfirmationMode = "authorize" | "execute" | "scenario" | "";

interface DevOpsQueueRow extends Record<string, unknown> {
  id: string;
  subject: AuthoritySubject;
  type: string;
  kind: "Operation Plan" | "Action Card";
  authority: string;
  status: string;
  contentHash: string;
  createdAt: string;
  ownership: DevOpsSubjectOwnership;
  execution: OperationExecution | null;
}

interface BaselineDifference {
  label: string;
  active: string;
  compared: string;
}

const route = useRoute();
const router = useRouter();
const store = useDevOpsWorkspaceStore();
const {
  selectedID: inspectorID,
  triggerElement: inspectorTrigger,
  open: openInspector,
  close: closeInspector,
  openFull: openFullDetail,
} = useWorkspaceInspector({
  selectedKey: "selected",
  resolveTrigger: (subjectID) => document.querySelector<HTMLElement>(`[data-devops-row-id="${CSS.escape(subjectID)}"]`)?.closest("button") ?? null,
});
let controller: AbortController | undefined;
let pollTimer: number | undefined;

const workspace = computed(() => store.workspace);
const plans = computed(() => workspace.value?.operation_plans ?? []);
const cards = computed(() => workspace.value?.action_cards ?? []);
const subjects = computed<AuthoritySubject[]>(() => [...plans.value, ...cards.value]);
const executions = computed(() => workspace.value?.executions ?? []);
const activeView = computed<WorkspaceView>(() => queryValue(route.query.view) === "identity" ? "identity" : "operations");
const requestedSubjectID = computed(() => queryValue(route.query.subject));
const requestedOperationID = computed(() => queryValue(route.query.operation));
const requestedBaselineID = computed(() => queryValue(route.query.baseline));
const requestedExecution = computed(() => executions.value.find((item) => item.id === requestedOperationID.value) ?? null);
const detailSubjectID = computed(() => requestedSubjectID.value || requestedExecution.value?.subject_id || "");
const detailSubject = computed(() => subjects.value.find((item) => item.id === detailSubjectID.value) ?? null);
const detailExecution = computed(() => requestedExecution.value
  ?? executions.value.find((item) => item.subject_id === detailSubjectID.value)
  ?? null);
const fullDetailRequested = computed(() => activeView.value === "operations"
  && !inspectorID.value
  && Boolean(requestedSubjectID.value || requestedOperationID.value));
const detailTargetInvalid = computed(() => fullDetailRequested.value
  && store.loaded
  && !detailSubject.value
  && !detailExecution.value);
const detailOwnership = computed(() => ownershipFor(detailSubject.value, detailExecution.value));
const detailAuthorization = computed(() => detailSubject.value?.authorization ?? null);
const authorizationExpired = computed(() => {
  const expiresAt = detailAuthorization.value?.expires_at || detailSubject.value?.expires_at;
  const parsed = Date.parse(expiresAt || "");
  return Number.isFinite(parsed) && parsed <= Date.now();
});
const authorizationCurrent = computed(() => Boolean(
  detailSubject.value
  && detailAuthorization.value
  && detailAuthorization.value.authorized_content_hash === detailSubject.value.content_hash
  && !authorizationExpired.value,
));
const canAuthorize = computed(() => detailOwnership.value.kind === "non_incident"
  && detailSubject.value?.status === "proposed");
const canExecute = computed(() => detailOwnership.value.kind === "non_incident"
  && detailSubject.value?.status === "authorized"
  && authorizationCurrent.value
  && !executions.value.some((item) => item.subject_id === detailSubject.value?.id));
const subjectExecutions = computed(() => executions.value.filter((item) => item.subject_id === detailSubjectID.value));
const selectedPayload = computed(() => materialPayload(detailSubject.value));

const inspectorSubject = computed(() => subjects.value.find((item) => item.id === inspectorID.value) ?? null);
const inspectorExecution = computed(() => executions.value.find((item) => item.subject_id === inspectorID.value) ?? null);
const inspectorOwnership = computed(() => ownershipFor(inspectorSubject.value, inspectorExecution.value));
const inspectorDelivery = computed(() => deliveryFor(inspectorOwnership.value.incidentID));
const inspectorTargetState = computed(() => inspectorID.value && store.loaded && !inspectorSubject.value ? "invalid" : "ready");

const proposedCount = computed(() => subjects.value.filter((item) => item.status === "proposed").length);
const authorizedCount = computed(() => subjects.value.filter((item) => item.status === "authorized").length);
const verifiedCount = computed(() => executions.value.filter((item) => item.verification?.status === "passed").length);
const queueRows = computed<DevOpsQueueRow[]>(() => subjects.value.map((subject) => {
  const execution = executions.value.find((item) => item.subject_id === subject.id) ?? null;
  return {
    id: subject.id,
    subject,
    type: subjectType(subject),
    kind: (isOperationPlan(subject) ? "Operation Plan" : "Action Card") as DevOpsQueueRow["kind"],
    authority: subject.authority,
    status: subject.status,
    contentHash: subject.content_hash,
    createdAt: subject.created_at,
    ownership: classifyDevOpsSubject(subject, executions.value, store.investigations),
    execution,
  };
}).sort((left, right) => queuePriority(left) - queuePriority(right)
  || Date.parse(right.createdAt) - Date.parse(left.createdAt)));

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
  if (!scenarioDeployment.value || !scenarioID.value) return "Scenario Deployment 不可用。";
  if (scenarioDeployment.value.workload?.desired_replicas === 0) return "Scenario fault 已恢复到 0 replicas。";
  if (!scenarioDeployment.value.resource_version) return "Kubernetes 投影缺少 resourceVersion。";
  if (!scenarioInvestigation.value) return "尚无同一 Scenario 的已完成 Agent 调查与 Evidence。";
  if (scenarioFreeze.value?.enabled) return "当前目标已进入 Change Freeze。";
  if (scenarioPlan.value) return `该 Scenario 已有状态为“${devopsStatusLabel(scenarioPlan.value.status)}”的 Operation Plan。`;
  return "";
});

const selectedDelivery = computed(() => deliveryFor(detailOwnership.value.incidentID));
const selectedPullRequestURL = computed(() => safeExternalURL(selectedDelivery.value?.pull_request_url));
const deliveryStages = computed(() => deliveryStageRows(selectedDelivery.value));
const technicalLinks = computed(() => (detailExecution.value?.links ?? []).filter((link) => (
  link.kind !== "incident" && link.kind !== "verification" && link.href.startsWith("/")
)));

const candidates = computed(() => workspace.value?.change_candidates ?? []);
const baselines = computed(() => workspace.value?.deployment_baselines ?? []);
const deliveries = computed(() => workspace.value?.deliveries ?? []);
const activeBaseline = computed(() => baselines.value.find((item) => item.status === "active") ?? baselines.value[0] ?? null);
const comparedBaseline = computed(() => baselines.value.find((item) => item.id === requestedBaselineID.value) ?? null);
const historicalBaselines = computed(() => baselines.value.filter((item) => item.id !== activeBaseline.value?.id));
const baselineDifferences = computed<BaselineDifference[]>(() => {
  const active = activeBaseline.value;
  const compared = comparedBaseline.value;
  if (!active || !compared || active.id === compared.id) return [];
  return [
    ["Source revision", active.source_revision, compared.source_revision],
    ["Image digest", active.image_digest, compared.image_digest],
    ["GitOps revision", active.gitops_revision, compared.gitops_revision],
    ["Configuration", active.config_hash, compared.config_hash],
    ["Verification", active.verification_hash, compared.verification_hash],
  ].filter(([, current, previous]) => current !== previous)
    .map(([label, current, previous]) => ({ label, active: current, compared: previous }));
});
const providerReadyCount = computed(() => workspace.value?.providers.filter((provider) => providerOutcome(provider) === "PASS").length ?? 0);
const providerConcernCount = computed(() => workspace.value?.providers.filter((provider) => providerOutcome(provider) === "FAIL").length ?? 0);
const failedExecutionCount = computed(() => executions.value.filter((item) => ["failed", "precondition_failed", "verification_failed"].includes(item.status)).length);
const frozenCount = computed(() => workspace.value?.change_freezes.filter((item) => item.enabled).length ?? 0);
const hasOperationalAttention = computed(() => (
  proposedCount.value + store.activeExecutions.length + failedExecutionCount.value + frozenCount.value
) > 0);

const tabs = [
  { label: "操作与授权", value: "operations", icon: "i-lucide-shield-check" },
  { label: "交付身份", value: "identity", icon: "i-lucide-git-commit-horizontal" },
];
const confirmationMode = ref<ConfirmationMode>("");
const confirmationSubject = computed(() => detailSubject.value);
const confirmationTitle = computed(() => {
  if (confirmationMode.value === "authorize") return "负责人审查 · 精确授权";
  if (confirmationMode.value === "execute") return isOperationPlan(confirmationSubject.value) ? "执行高影响 Operation Plan" : "执行本地可逆动作";
  return "创建不可变 Scenario 恢复计划";
});
const confirmationDescription = computed(() => {
  if (confirmationMode.value === "authorize") return "Authorization 只绑定当前内容 Hash；材料变化后必须重新审查。";
  if (confirmationMode.value === "execute") return "Worker 会再次检查授权、有效期、精确 Hash 与前置条件。";
  return "仅创建持久化 Plan，不授权也不执行 Kubernetes mutation。";
});
const confirmationTarget = computed(() => {
  if (confirmationMode.value === "scenario") return "demo/Deployment/cloudops-scenario-fault";
  return subjectTarget(confirmationSubject.value);
});
const confirmationEffect = computed(() => {
  if (confirmationMode.value === "authorize") return "绑定本地负责人审查与当前精确 Subject。";
  if (confirmationMode.value === "execute") return "按已授权材料排队执行；排队不代表 Provider 已观测或已验证。";
  return "创建 replicas=0 的恢复计划；不产生 Provider 外部副作用。";
});
const confirmationAuthority = computed(() => {
  if (confirmationMode.value === "scenario") return "仅创建计划 · 未授权";
  if (confirmationMode.value === "authorize") return confirmationSubject.value?.authority ?? "未记录";
  return detailAuthorization.value
    ? `${detailAuthorization.value.authorized_by} · ${detailAuthorization.value.id}`
    : "未授权";
});
const confirmationVersion = computed(() => {
  if (confirmationMode.value === "scenario") return scenarioDeployment.value?.resource_version ?? "未记录";
  if (isOperationPlan(confirmationSubject.value)) return confirmationSubject.value.configuration_revision_id;
  return confirmationSubject.value?.run_id ?? "未记录";
});
const confirmationHash = computed(() => confirmationMode.value === "scenario" ? "" : confirmationSubject.value?.content_hash ?? "");
const confirmationRecovery = computed(() => confirmationMode.value === "scenario"
  ? "删除或拒绝计划不会回滚任何 Provider 状态；执行仍需独立授权。"
  : confirmationSubject.value?.risk || "恢复能力由当前 subject 与 Provider adapter 决定。",
);
const confirmationPending = computed(() => Boolean(store.mutatingSubjectID));

function queryValue(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw : "";
}

function isOperationPlan(subject: AuthoritySubject | null): subject is OperationPlan {
  return subject?.authority === "high_impact";
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function subjectType(subject: AuthoritySubject): string {
  return isOperationPlan(subject) ? subject.operation_type : subject.action_type;
}

function materialPayload(subject: AuthoritySubject | null): Record<string, unknown> {
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
}

function ownershipFor(subject: AuthoritySubject | null, execution: OperationExecution | null): DevOpsSubjectOwnership {
  if (subject) return classifyDevOpsSubject(subject, executions.value, store.investigations);
  if (execution?.incident_id) {
    return { kind: "incident", incidentID: execution.incident_id, reason: "当前 execution 已绑定 Incident。" };
  }
  if (execution) return classifyDevOpsRun(execution.run_id, store.investigations);
  return { kind: "unknown", incidentID: "", reason: "当前 Query 未解析到 authority subject 或 execution。" };
}

function ownershipLabel(ownership: DevOpsSubjectOwnership): string {
  if (ownership.kind === "incident") return "Incident 所有";
  if (ownership.kind === "non_incident") return "DevOps 所有";
  return "所有权未知";
}

function ownershipColor(kind: DevOpsSubjectOwnership["kind"]): "warning" | "info" | "neutral" {
  if (kind === "incident") return "warning";
  if (kind === "non_incident") return "info";
  return "neutral";
}

function formatUTC(value?: string): string {
  if (!value) return "未记录";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间无效" : date.toISOString();
}

function compactIdentity(value?: string, size = 8): string {
  if (!value) return "未记录";
  if (value.length <= size * 2 + 1) return value;
  return `${value.slice(0, size)}…${value.slice(-size)}`;
}

function providerOutcome(provider: ProviderBranch): "PASS" | "FAIL" | "NOT RUN" {
  if (provider.state === "available") return "PASS";
  if (!provider.enabled || provider.state === "disabled" || provider.state === "not_configured") return "NOT RUN";
  return "FAIL";
}

function providerTone(provider: ProviderBranch): "success" | "warning" | "neutral" {
  const outcome = providerOutcome(provider);
  if (outcome === "PASS") return "success";
  if (outcome === "FAIL") return "warning";
  return "neutral";
}

function providerOutcomeLabel(provider: ProviderBranch): string {
  const outcome = providerOutcome(provider);
  if (outcome === "PASS") return "可用";
  if (outcome === "FAIL") return "需要诊断";
  return "未运行";
}

function devopsStatusLabel(value?: string): string {
  const normalized = value?.trim().toLowerCase() ?? "";
  const labels: Record<string, string> = {
    active: "已生效",
    approved: "已批准",
    authorized: "已授权",
    cancelled: "已取消",
    completed: "已完成",
    disabled: "未启用",
    expired: "已过期",
    failed: "执行失败",
    frozen: "已冻结",
    healthy: "健康",
    merged: "已合并",
    not_authorized: "未授权",
    not_configured: "未配置",
    not_run: "尚未运行",
    observed: "已观测",
    open: "进行中",
    passed: "验证通过",
    precondition_failed: "前置条件失败",
    proposed: "待审批",
    ready: "等待执行",
    rejected: "已拒绝",
    running: "执行中",
    succeeded: "已成功",
    success: "成功",
    synced: "已同步",
    verification_failed: "验证失败",
    warning: "已冻结",
  };
  return labels[normalized] ?? (value?.replace(/_/g, " ") || "状态未知");
}

function providerCausalLabel(): string {
  const total = workspace.value?.providers.length ?? 0;
  if (!total) return "Provider 尚未配置";
  return `${providerReadyCount.value}/${total} 个已就绪`;
}

function countOrEmpty(count: number, present: string, empty: string): string {
  return count ? `${count} ${present}` : empty;
}

function humanOperation(value: string): string {
  const labels: Record<string, string> = {
    "kubernetes.deployment.scale": "调整 Deployment 副本",
    "local.change_freeze.set": "设置变更冻结",
  };
  return labels[value] ?? value.replace(/[._]/g, " ");
}

function queuePriority(row: DevOpsQueueRow): number {
  if (row.execution && ["failed", "precondition_failed", "verification_failed"].includes(row.execution.status)) return 0;
  if (row.execution?.status === "running" || row.execution?.status === "ready") return 1;
  if (row.status === "proposed") return 2;
  if (row.status === "authorized") return 3;
  return 4;
}

function queueSeverity(row: DevOpsQueueRow): DenseListSeverity {
  if (row.execution && ["failed", "precondition_failed", "verification_failed"].includes(row.execution.status)) return "critical";
  if (row.status === "proposed" || row.ownership.kind !== "non_incident") return "warning";
  if (row.execution?.verification?.status === "passed") return "success";
  if (row.execution?.status === "running" || row.execution?.status === "ready") return "info";
  return "neutral";
}

function queuePhase(row: DevOpsQueueRow): string {
  if (row.execution?.verification) return devopsStatusLabel(row.execution.verification.status);
  if (row.execution) return devopsStatusLabel(row.execution.status);
  if (row.status === "authorized") return "已授权，等待执行";
  if (row.status === "proposed") return "等待负责人审批";
  return devopsStatusLabel(row.status);
}

function queueNextStep(row: DevOpsQueueRow): string {
  if (row.ownership.kind === "incident") return "前往 Incident 继续审批、交付或验证";
  if (row.ownership.kind === "unknown") return "刷新投影并确认操作归属后才能继续";
  if (row.execution && ["failed", "precondition_failed", "verification_failed"].includes(row.execution.status)) return "查看失败身份与恢复路径";
  if (row.execution?.status === "running" || row.execution?.status === "ready") return "等待 Provider 观测与验证";
  if (row.status === "proposed") return "核对风险、目标与精确 Hash";
  if (row.status === "authorized") return "核对有效授权后排队执行";
  return "查看完整因果链";
}

function queueRevision(row: DevOpsQueueRow): string {
  const revision = isOperationPlan(row.subject) ? row.subject.configuration_revision_id : row.subject.run_id;
  return compactIdentity(revision, 7);
}

function queueChangeSummary(row: DevOpsQueueRow): string {
  const parameters = objectValue(row.subject.parameters);
  const replicas = parameters?.replicas;
  if (typeof replicas === "number") return `目标副本调整为 ${replicas}`;
  return row.kind;
}

function queueExecutionIdentity(row: DevOpsQueueRow): string {
  return row.execution ? compactIdentity(row.execution.id, 7) : "尚未执行";
}

function queueVerificationFact(row: DevOpsQueueRow): string {
  if (row.execution?.verification?.status === "passed") return "验证通过";
  if (row.execution?.verification?.status === "failed") return "验证失败";
  return row.execution ? "等待验证" : "";
}

function baselineTarget(item: DeploymentBaseline): string {
  return `${item.cluster}/${item.namespace}/${item.workload_kind}/${item.workload_name}`;
}

function baselineTechnicalFields(item: DeploymentBaseline): TechnicalDetailField[] {
  return [
    { label: "Baseline ID", value: item.id, code: true, copyValue: item.id },
    { label: "Target identity", value: item.target_identity_hash, code: true, copyValue: item.target_identity_hash },
    { label: "Source revision", value: item.source_revision, code: true, copyValue: item.source_revision },
    { label: "Image digest", value: item.image_digest, code: true, copyValue: item.image_digest },
    { label: "GitOps revision", value: item.gitops_revision, code: true, copyValue: item.gitops_revision },
    { label: "Configuration hash", value: item.config_hash, code: true, copyValue: item.config_hash },
    { label: "Verification hash", value: item.verification_hash, code: true, copyValue: item.verification_hash },
    { label: "Verified UTC", value: formatUTC(item.verified_at), code: true },
  ];
}

function candidateTechnicalFields(item: ChangeCandidate): TechnicalDetailField[] {
  return [
    { label: "Candidate ID", value: item.id, code: true, copyValue: item.id },
    { label: "Change reference", value: item.change_ref, code: true, copyValue: item.change_ref },
    { label: "Source revision", value: item.commit_sha, code: true, copyValue: item.commit_sha },
    { label: "Image digest", value: item.image_digest, code: true, copyValue: item.image_digest },
    { label: "GitOps revision", value: item.gitops_revision, code: true, copyValue: item.gitops_revision },
    { label: "Evidence hash", value: item.content_hash, code: true, copyValue: item.content_hash },
  ];
}

function deliveryPullRequestURL(item: DeliveryProjection): string {
  return safeExternalURL(item.pull_request_url);
}

function subjectTarget(subject: AuthoritySubject | null): string {
  const target = objectValue(subject?.target);
  if (!target) return subject ? subjectType(subject) : "未记录";
  const identity = [target.cluster_id, target.namespace, target.workload_kind, target.workload_name]
    .filter((value): value is string => typeof value === "string" && Boolean(value));
  return identity.join("/") || subjectType(subject as AuthoritySubject);
}

function setView(value: string | number) {
  if (value !== "operations" && value !== "identity") return;
  void router.replace({
    path: route.path,
    query: { ...route.query, view: value === "operations" ? undefined : value, selected: undefined },
    hash: "",
  });
}

function selectQueueRow(row: DevOpsQueueRow, trigger: HTMLElement | null) {
  void openInspector(row.id, trigger);
}

function selectBaseline(item: DeploymentBaseline) {
  void router.replace({
    path: route.path,
    query: { ...route.query, view: "identity", baseline: item.id },
    hash: "",
  });
}

function handleInspectorOpenChange(open: boolean) {
  if (!open) void closeInspector();
}

function enterFullDetail() {
  const subject = inspectorSubject.value;
  if (!subject) return;
  const execution = inspectorExecution.value;
  void openFullDetail({
    path: route.path,
    query: {
      ...route.query,
      view: "operations",
      selected: undefined,
      subject: subject.id,
      operation: execution?.id,
    },
    hash: "",
  });
}

function returnToQueue() {
  void router.push({
    path: route.path,
    query: { ...route.query, view: undefined, selected: undefined, subject: undefined, operation: undefined },
    hash: "",
  });
}

function selectExecution(execution: OperationExecution) {
  void router.replace({
    path: route.path,
    query: { ...route.query, view: "operations", selected: undefined, subject: execution.subject_id, operation: execution.id },
    hash: route.hash,
  });
}

function openConfirmation(mode: Exclude<ConfirmationMode, "">) {
  if (mode === "authorize" && !canAuthorize.value) return;
  if (mode === "execute" && !canExecute.value) return;
  if (mode === "scenario" && !canProposeScenarioPlan.value) return;
  confirmationMode.value = mode;
}

async function confirmCommand(reason: string) {
  const mode = confirmationMode.value;
  const subject = detailSubject.value;
  if (mode === "authorize" && subject) {
    if (isOperationPlan(subject)) await store.authorizePlan(subject, reason);
    else await store.authorizeCard(subject, reason);
  } else if (mode === "execute" && subject) {
    const execution = isOperationPlan(subject)
      ? await store.executePlan(subject)
      : await store.executeCard(subject);
    if (execution) {
      await router.replace({
        path: route.path,
        query: { ...route.query, view: "operations", subject: subject.id, operation: execution.id },
      });
    }
  } else if (mode === "scenario") {
    await proposeScenarioRecovery();
  }
  if (!store.error) confirmationMode.value = "";
}

async function proposeScenarioRecovery() {
  const deployment = scenarioDeployment.value;
  const run = scenarioInvestigation.value;
  const resourceVersion = deployment?.resource_version;
  if (!canProposeScenarioPlan.value || !deployment || !run || !resourceVersion || !scenarioID.value || !store.scenarioResources) return;
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
  if (plan) {
    await router.replace({
      path: route.path,
      query: { ...route.query, view: "operations", selected: undefined, subject: plan.id, operation: undefined },
      hash: "",
    });
  }
}

async function refresh() {
  store.clearFeedback();
  await store.load(true);
}

function deliveryFor(incidentID: string): DeliveryProjection | null {
  if (!incidentID) return null;
  return workspace.value?.deliveries.find((item) => item.incident_id === incidentID) ?? null;
}

function deliveryStageRows(delivery: DeliveryProjection | null) {
  if (!delivery) return [];
  return [
    { label: "PR 草案", status: delivery.pull_request_state || "not_run", detail: delivery.pull_request_number ? `PR #${delivery.pull_request_number}` : "尚未运行" },
    { label: "必需 CI", status: delivery.ci_status || "not_run", detail: devopsStatusLabel(delivery.ci_status || "not_run") },
    { label: "人工合并", status: delivery.merged_commit_sha ? "observed" : "not_run", detail: compactIdentity(delivery.merged_commit_sha, 10) },
    { label: "Argo 同步", status: delivery.argo_sync_status || "not_run", detail: [delivery.argo_operation_phase, delivery.argo_health_status].filter(Boolean).map(devopsStatusLabel).join(" / ") || "尚未运行" },
    { label: "Rollout", status: delivery.status || "not_run", detail: `可用副本 ${delivery.available_replicas}/${delivery.desired_replicas}` },
  ];
}

onMounted(async () => {
  controller = new AbortController();
  await store.load(false, controller.signal);
  pollTimer = window.setInterval(() => {
    if (document.visibilityState === "visible" && store.activeExecutions.length && !store.loading && !store.mutatingSubjectID) {
      void store.load(true);
    }
  }, 1500);
});

onBeforeUnmount(() => {
  controller?.abort();
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
});
</script>

<template>
  <WorkspacePageFrame
    as="section"
    class="devops-workspace"
    aria-labelledby="devops-heading"
  >
    <WorkspaceHeader
      heading-id="devops-heading"
      eyebrow="交付控制"
      title="DevOps Workspace"
      description="从 Provider 事实到当前 Deployment Baseline，审查非事故操作的完整交付因果链。"
    >
      <template #context>
        <div
          class="header-facts"
          aria-label="DevOps 投影摘要"
        >
          <template v-if="hasOperationalAttention">
            <span><strong>{{ proposedCount }}</strong> 待审批</span>
            <span><strong>{{ store.activeExecutions.length }}</strong> 执行中</span>
            <span><strong>{{ failedExecutionCount }}</strong> 失败</span>
            <span><strong>{{ frozenCount }}</strong> 已冻结</span>
          </template>
          <span v-else class="header-facts__healthy"><UIcon name="i-lucide-circle-check" />当前没有待审批、执行失败或变更冻结项</span>
          <span>{{ activeBaseline ? "当前 Baseline 已生效" : "尚无有效 Baseline" }}</span>
        </div>
      </template>
      <template #actions>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          :loading="store.loading"
          label="刷新"
          @click="refresh"
        />
      </template>
    </WorkspaceHeader>

    <WorkspaceStatusRow
      v-if="workspace"
      :tone="providerConcernCount ? 'warning' : providerReadyCount ? 'success' : 'neutral'"
      :icon="providerConcernCount ? 'i-lucide-triangle-alert' : 'i-lucide-plug-zap'"
      title="Provider 连接摘要"
      :description="providerConcernCount ? `${providerConcernCount} 个 Provider 需要诊断；不可用分支不会被解释为已交付。` : `${providerReadyCount}/${workspace.providers.length} 个 Provider 当前可用。`"
      :badge="providerConcernCount ? `${providerConcernCount} 个需关注` : `${providerReadyCount} 个已就绪`"
    >
      <template #meta>
        <time :datetime="workspace.collected_at">{{ formatUTC(workspace.collected_at) }}</time>
      </template>
      <template #actions>
        <UPopover>
          <UTooltip text="查看 Provider 诊断">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-list-tree"
              square
              aria-label="查看 Provider 诊断"
            />
          </UTooltip>
          <template #content>
            <div class="provider-popover">
              <header><strong>Provider 诊断</strong><span>只读投影</span></header>
              <ul>
                <li
                  v-for="provider in workspace.providers"
                  :key="provider.provider"
                >
                  <span :class="`provider-dot provider-dot--${providerTone(provider)}`" />
                  <div><strong>{{ provider.provider }}</strong><small>{{ provider.detail }}</small></div>
                  <ResultBadge :result="providerOutcome(provider)" :label="providerOutcomeLabel(provider)" />
                </li>
              </ul>
              <WorkspaceTechnicalDetails
                title="Provider 配置身份"
                description="完整 revision 仅用于核对和复制"
                :fields="workspace.providers.map((provider) => ({ label: provider.provider, value: provider.configuration_revision_id, code: true, copyValue: provider.configuration_revision_id }))"
              />
            </div>
          </template>
        </UPopover>
      </template>
    </WorkspaceStatusRow>

    <UAlert
      v-if="store.error"
      color="error"
      variant="soft"
      icon="i-lucide-circle-alert"
      :title="store.failure?.code || 'DEVOPS_REQUEST_FAILED'"
      :description="store.failure?.message || store.error"
    >
      <template #actions>
        <div class="error-identities">
          <code v-if="store.failure?.requestID">Request {{ store.failure.requestID }}</code>
          <code v-if="store.failure?.traceID">Trace {{ store.failure.traceID }}</code>
        </div>
      </template>
    </UAlert>
    <UAlert
      v-else-if="store.notice"
      color="success"
      variant="soft"
      icon="i-lucide-circle-check"
      title="命令已受理"
      :description="store.notice"
    />

    <section
      class="devops-view-switcher"
      aria-label="DevOps Workspace 视图"
    >
      <UTabs
        :model-value="activeView"
        :items="tabs"
        :content="false"
        color="primary"
        variant="pill"
        @update:model-value="setView"
      />
      <div class="toolbar-summary">
        <strong>{{ activeView === "operations" ? "操作与授权" : "交付身份与 Baseline" }}</strong>
        <span>{{ activeView === "operations" ? "高风险与待行动状态优先" : "当前生效 Baseline 优先" }}</span>
      </div>
      <UButton
        v-if="fullDetailRequested"
        color="neutral"
        variant="ghost"
        icon="i-lucide-list"
        label="返回全局队列"
        @click="returnToQueue"
      />
    </section>

    <div
      v-if="!workspace && store.loading"
      class="workspace-loading"
      role="status"
      aria-label="正在加载 DevOps Workspace"
    >
      <USkeleton
        v-for="index in 7"
        :key="index"
        class="loading-row"
      />
    </div>
    <WorkspaceState
      v-else-if="!workspace"
      kind="empty"
      title="当前没有 DevOps 投影"
      description="没有加载到持久化 Operation、交付身份或 Provider 分支。"
    />

    <main
      v-else-if="activeView === 'operations' && !fullDetailRequested"
      class="operations-index"
    >
      <section
        class="attention-strip"
        :class="{ 'is-clear': !hasOperationalAttention }"
        aria-labelledby="attention-heading"
      >
        <header>
          <div><span>行动优先</span><h2 id="attention-heading">需要关注</h2></div>
          <p>待审批、执行中、失败和冻结状态优先；正常历史不会占用首屏。</p>
        </header>
        <div v-if="!hasOperationalAttention" class="attention-clear">
          <UIcon name="i-lucide-shield-check" aria-hidden="true" />
          <div><strong>当前没有待审批、执行失败或变更冻结项</strong><span>操作队列处于稳定状态。</span></div>
        </div>
        <dl v-else>
          <div :class="{ 'has-attention': proposedCount }"><dt>待审批</dt><dd>{{ proposedCount }}</dd><small>{{ proposedCount ? "核对风险与精确 Hash" : "当前无待审批项" }}</small></div>
          <div :class="{ 'is-running': store.activeExecutions.length }"><dt>执行中</dt><dd>{{ store.activeExecutions.length }}</dd><small>{{ store.activeExecutions.length ? "等待 Provider 观测" : "当前无执行中操作" }}</small></div>
          <div :class="{ 'has-critical': failedExecutionCount }"><dt>失败</dt><dd>{{ failedExecutionCount }}</dd><small>{{ failedExecutionCount ? "需要检查失败身份与恢复" : "当前无执行失败" }}</small></div>
          <div :class="{ 'has-attention': frozenCount }"><dt>冻结</dt><dd>{{ frozenCount }}</dd><small>{{ frozenCount ? "目标保持关闭保护" : "当前无生效冻结" }}</small></div>
        </dl>
      </section>

      <section
        class="causal-section"
        aria-labelledby="causal-heading"
      >
        <header class="section-heading">
          <div><span>交付因果关系</span><h2 id="causal-heading">从 Provider 到验证基线</h2></div>
          <p>每一步只呈现当前真实投影；缺失事实保持明确的未运行状态。</p>
        </header>
        <ol class="causal-chain" aria-label="交付因果链">
          <li><span><UIcon name="i-lucide-plug-zap" /></span><div><small>01</small><strong>Provider</strong><em>{{ providerCausalLabel() }}</em></div></li>
          <li><span><UIcon name="i-lucide-git-compare-arrows" /></span><div><small>02</small><strong>Change</strong><em>{{ countOrEmpty(deliveries.length, "项变更已观测", "未检测到变更") }}</em></div></li>
          <li><span><UIcon name="i-lucide-file-diff" /></span><div><small>03</small><strong>Candidate</strong><em>{{ countOrEmpty(candidates.length, "个候选项", "尚未生成候选项") }}</em></div></li>
          <li><span><UIcon name="i-lucide-list-checks" /></span><div><small>04</small><strong>Operation</strong><em>{{ countOrEmpty(subjects.length, "项操作", "尚未生成操作") }}</em></div></li>
          <li><span><UIcon name="i-lucide-file-key-2" /></span><div><small>05</small><strong>Authority</strong><em>{{ countOrEmpty(authorizedCount, "项已授权", "尚未授权") }}</em></div></li>
          <li><span><UIcon name="i-lucide-play" /></span><div><small>06</small><strong>Execution</strong><em>{{ countOrEmpty(executions.length, "项已执行", "尚未执行") }}</em></div></li>
          <li><span><UIcon name="i-lucide-shield-check" /></span><div><small>07</small><strong>Verification</strong><em>{{ countOrEmpty(verifiedCount, "项已验证", "尚未验证") }}</em></div></li>
          <li><span><UIcon name="i-lucide-git-commit-horizontal" /></span><div><small>08</small><strong>Baseline</strong><em>{{ activeBaseline ? "当前基线已生效" : "尚无有效基线" }}</em></div></li>
        </ol>
      </section>

      <section
        class="queue-section"
        aria-labelledby="queue-heading"
        data-testid="devops-global-queue"
      >
        <header class="section-heading">
          <div>
            <span>全局非 Incident 操作</span><h2 id="queue-heading">Authority Queue</h2>
            <p>按失败、执行中和待审批排序；选择操作查看阶段、所有权与下一步。</p>
          </div>
          <UBadge
            color="neutral"
            variant="soft"
            :label="`${queueRows.length} 项`"
          />
        </header>
        <WorkspaceDenseList
          :items="queueRows"
          :item-key="(row) => row.id"
          label="DevOps Authority Queue"
          :selected-key="inspectorID"
          :severity="queueSeverity"
          empty="当前没有持久化 Operation Plan 或 Action Card。"
          @select="selectQueueRow"
        >
          <template #leading="{ item }">
            <span class="queue-icon" aria-hidden="true"><UIcon :name="item.kind === 'Operation Plan' ? 'i-lucide-file-key-2' : 'i-lucide-shield-check'" /></span>
          </template>
          <template #title="{ item }">
            <span
              class="dense-data-table-row"
              data-testid="devops-row-type"
              :data-devops-row-id="item.id"
            >{{ humanOperation(item.type) }}</span>
          </template>
          <template #description="{ item }">
            <span class="queue-row-description">
              <span>{{ subjectTarget(item.subject) }} · {{ queueChangeSummary(item) }}</span>
              <small>Revision {{ queueRevision(item) }} · Execution {{ queueExecutionIdentity(item) }} · {{ formatUTC(item.execution?.completed_at || item.execution?.created_at || item.createdAt) }}</small>
            </span>
          </template>
          <template #meta="{ item }">
            <span class="queue-row-meta">
              <strong>{{ queuePhase(item) }}</strong>
              <small>{{ queueNextStep(item) }}</small>
            </span>
          </template>
          <template #trailing="{ item }">
            <span class="queue-status-stack">
              <ResultBadge
                :result="item.execution?.status || item.status"
                :label="devopsStatusLabel(item.execution?.status || item.status)"
              />
              <span>{{ ownershipLabel(item.ownership) }}</span>
              <small v-if="queueVerificationFact(item)">{{ queueVerificationFact(item) }}</small>
            </span>
          </template>
        </WorkspaceDenseList>
      </section>

      <WorkspaceStatusRow
        :tone="scenarioProposalBlocker ? 'neutral' : 'info'"
        icon="i-lucide-file-plus-2"
        title="Scenario 恢复计划"
        :description="scenarioProposalBlocker || 'Evidence、resourceVersion 与冻结前置条件已齐备；仅创建计划，不授权也不执行。'"
        :badge="scenarioPlan ? devopsStatusLabel(scenarioPlan.status) : '仅创建计划'"
      >
        <template #meta>
          <span>副本就绪 {{ scenarioDeployment?.workload?.ready_replicas ?? 0 }}/{{ scenarioDeployment?.workload?.desired_replicas ?? 0 }}</span>
        </template>
        <template #actions>
          <UButton
            color="warning"
            variant="soft"
            icon="i-lucide-file-plus-2"
            label="创建恢复计划"
            :disabled="!canProposeScenarioPlan || Boolean(store.mutatingSubjectID)"
            @click="openConfirmation('scenario')"
          />
        </template>
      </WorkspaceStatusRow>

      <section class="freeze-section" aria-labelledby="freeze-heading">
        <header class="section-heading compact-heading">
          <div><span>本地安全边界</span><h2 id="freeze-heading">Change Freeze</h2></div>
          <UBadge color="neutral" variant="soft" :label="workspace.change_freezes.length ? `${workspace.change_freezes.length} 条` : '无记录'" />
        </header>
        <ul v-if="workspace.change_freezes.length">
          <li v-for="freeze in workspace.change_freezes" :key="`${freeze.target.cluster_id}/${freeze.target.namespace}/${freeze.target.workload_name}`">
            <span class="freeze-icon"><UIcon :name="freeze.enabled ? 'i-lucide-lock-keyhole' : 'i-lucide-lock-keyhole-open'" /></span>
            <div><strong>{{ freeze.target.namespace }}/{{ freeze.target.workload_name }}</strong><small>{{ freeze.reason || "未记录原因" }}</small></div>
            <ResultBadge :result="freeze.enabled ? 'warning' : 'success'" :label="freeze.enabled ? '已冻结' : '未冻结'" />
            <code>row v{{ freeze.row_version }}</code>
          </li>
        </ul>
        <p v-else class="inline-empty inline-empty--healthy"><UIcon name="i-lucide-shield-check" aria-hidden="true" />当前没有 Change Freeze 风险。</p>
      </section>
    </main>

    <main
      v-else-if="activeView === 'operations'"
      class="operation-detail"
      data-testid="devops-full-detail"
    >
      <WorkspaceState
        v-if="detailTargetInvalid"
        kind="invalid"
        title="DevOps 查询无法恢复目标"
        :description="`subject=${requestedSubjectID || '未提供'} · operation=${requestedOperationID || '未提供'}`"
      >
        <template #actions>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-list"
            label="返回全局队列"
            @click="returnToQueue"
          />
        </template>
      </WorkspaceState>
      <template v-else>
        <header class="detail-heading">
          <div>
            <span>完整详情 · {{ detailSubject ? (isOperationPlan(detailSubject) ? "Operation Plan" : "Action Card") : "Execution" }}</span>
            <h2>{{ detailSubject ? subjectType(detailSubject) : detailExecution?.operation_type }}</h2>
            <code translate="no">{{ detailSubject?.id || detailExecution?.subject_id }}</code>
          </div>
          <div>
            <UBadge
              :color="ownershipColor(detailOwnership.kind)"
              variant="soft"
              :label="ownershipLabel(detailOwnership)"
            />
            <ResultBadge
              :result="detailSubject?.status || detailExecution?.status || 'unknown'"
              :label="devopsStatusLabel(detailSubject?.status || detailExecution?.status)"
            />
          </div>
        </header>

        <UAlert
          v-if="detailOwnership.kind === 'incident'"
          color="warning"
          variant="soft"
          icon="i-lucide-route"
          title="事故操作由 Incident 单一生命周期拥有"
          :description="detailOwnership.reason"
          data-testid="incident-ownership-boundary"
        />
        <UAlert
          v-else-if="detailOwnership.kind === 'unknown'"
          color="error"
          variant="soft"
          icon="i-lucide-shield-x"
          title="所有权无法证明，写入口保持关闭"
          :description="detailOwnership.reason"
        />

        <nav
          v-if="detailOwnership.incidentID"
          class="incident-stage-links"
          aria-label="Incident 生命周期阶段"
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-file-key-2"
            label="Incident 审批"
            :to="incidentStageHref(detailOwnership.incidentID, 'approval')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-git-pull-request-arrow"
            label="Incident 交付"
            :to="incidentStageHref(detailOwnership.incidentID, 'delivery')"
          />
          <UButton
            color="primary"
            variant="soft"
            icon="i-lucide-shield-check"
            label="Incident 验证"
            :to="incidentStageHref(detailOwnership.incidentID, 'verification')"
          />
        </nav>

        <section
          class="detail-section authority-section"
          aria-labelledby="authority-heading"
        >
          <header>
            <div>
              <span>ExactIdentity / Authority</span><h3 id="authority-heading">
                不可变 Subject
              </h3>
            </div><ResultBadge
              :result="authorizationCurrent ? 'authorized' : authorizationExpired ? 'expired' : 'not_authorized'"
              :label="devopsStatusLabel(authorizationCurrent ? 'authorized' : authorizationExpired ? 'expired' : 'not_authorized')"
            />
          </header>
          <div class="hash-grid">
            <HashValue
              v-if="detailSubject"
              label="Content hash"
              :value="detailSubject.content_hash"
            />
            <HashValue
              v-if="detailAuthorization"
              label="Authorized hash"
              :value="detailAuthorization.authorized_content_hash"
            />
            <HashValue
              v-if="detailExecution"
              label="Expected execution hash"
              :value="detailExecution.expected_content_hash"
            />
          </div>
          <dl class="fact-grid">
            <div><dt>Authority</dt><dd>{{ detailSubject?.authority || "未加载 Subject" }}</dd></div>
            <div><dt>Configuration Revision</dt><dd><code>{{ isOperationPlan(detailSubject) ? detailSubject.configuration_revision_id : detailExecution?.configuration_revision_id || "run-bound" }}</code></dd></div>
            <div><dt>Subject expires UTC</dt><dd>{{ formatUTC(detailSubject?.expires_at) }}</dd></div>
            <div><dt>Authorized by</dt><dd>{{ detailAuthorization ? `${detailAuthorization.authorized_by} · ${detailAuthorization.reason}` : "未授权" }}</dd></div>
            <div><dt>Authorization expires UTC</dt><dd>{{ formatUTC(detailAuthorization?.expires_at) }}</dd></div>
            <div><dt>风险</dt><dd>{{ detailSubject?.risk || "未加载 Subject 风险" }}</dd></div>
          </dl>
          <JSONSnapshot
            v-if="detailSubject"
            title="精确材料载荷"
            :value="selectedPayload"
          />
          <div
            v-if="detailOwnership.kind === 'non_incident'"
            class="subject-actions"
            data-testid="non-incident-actions"
          >
            <UButton
              v-if="canAuthorize"
              color="warning"
              icon="i-lucide-file-key-2"
              label="授权精确 Hash"
              :loading="store.mutatingSubjectID === detailSubject?.id"
              @click="openConfirmation('authorize')"
            />
            <UButton
              v-if="canExecute"
              color="error"
              variant="soft"
              icon="i-lucide-play"
              label="排队执行"
              :loading="store.mutatingSubjectID === detailSubject?.id"
              @click="openConfirmation('execute')"
            />
            <span
              v-if="!canAuthorize && !canExecute"
              class="muted-copy"
            >当前状态没有可用 DevOps 命令。</span>
          </div>
        </section>

        <section
          class="detail-section execution-section"
          aria-labelledby="execution-heading"
        >
          <header>
            <div>
              <span>已受理 / 已分发 / 已观测</span><h3 id="execution-heading">
                Execution 与审计
              </h3>
            </div><UBadge
              color="neutral"
              variant="soft"
              :label="`${subjectExecutions.length || (detailExecution ? 1 : 0)} 次执行`"
            />
          </header>
          <nav
            v-if="subjectExecutions.length"
            class="execution-selector"
            aria-label="Operation execution attempts"
          >
            <UButton
              v-for="execution in subjectExecutions"
              :key="execution.id"
              :color="execution.id === detailExecution?.id ? 'primary' : 'neutral'"
              :variant="execution.id === detailExecution?.id ? 'soft' : 'ghost'"
              icon="i-lucide-activity"
              :label="`第 ${execution.attempt} 次 · ${devopsStatusLabel(execution.status)}`"
              @click="selectExecution(execution)"
            />
          </nav>
          <div
            v-if="detailExecution"
            class="execution-summary"
          >
            <dl class="fact-grid">
              <div><dt>Execution ID</dt><dd><code>{{ detailExecution.id }}</code></dd></div>
              <div><dt>状态</dt><dd><ResultBadge :result="detailExecution.status" :label="devopsStatusLabel(detailExecution.status)" /></dd></div>
              <div><dt>Attempt</dt><dd>{{ detailExecution.attempt }}</dd></div>
              <div><dt>Created UTC</dt><dd>{{ formatUTC(detailExecution.created_at) }}</dd></div>
              <div><dt>Effect boundary UTC</dt><dd>{{ formatUTC(detailExecution.external_effect_started_at) }}</dd></div>
              <div><dt>Completed UTC</dt><dd>{{ formatUTC(detailExecution.completed_at) }}</dd></div>
              <div v-if="detailExecution.failure_code">
                <dt>Failure</dt><dd>{{ detailExecution.failure_code }} · {{ detailExecution.failure_summary }}</dd>
              </div>
            </dl>
            <nav
              v-if="technicalLinks.length"
              class="technical-links"
              aria-label="Execution technical links"
            >
              <UButton
                v-for="link in technicalLinks"
                :key="`${link.kind}:${link.href}`"
                color="neutral"
                variant="outline"
                icon="i-lucide-link"
                :label="link.label"
                :to="link.href"
              />
            </nav>
            <ol
              v-if="detailExecution.events.length"
              class="audit-timeline"
            >
              <li
                v-for="event in detailExecution.events"
                :key="event.id"
              >
                <span>{{ event.sequence }}</span>
                <div>
                  <header><strong>{{ event.type }}</strong><time :datetime="event.occurred_at">{{ formatUTC(event.occurred_at) }}</time></header>
                  <HashValue
                    label="Audit content hash"
                    :value="event.content_hash"
                  />
                  <JSONSnapshot
                    title="Audit payload"
                    :value="event.payload"
                  />
                </div>
              </li>
            </ol>
            <p
              v-else
              class="empty-copy"
            >
              尚无 Execution 审计事件。
            </p>
          </div>
          <p
            v-else
            class="empty-copy"
          >
            Execution 尚未运行；没有已受理、已分发、已观测或已验证事实。
          </p>
        </section>

        <section
          class="detail-section delivery-section"
          aria-labelledby="delivery-rail-heading"
        >
          <header>
            <div>
              <span>线性交付观测</span><h3 id="delivery-rail-heading">
                Delivery Rail
              </h3>
            </div><ResultBadge :result="selectedDelivery?.status || 'not_run'" :label="devopsStatusLabel(selectedDelivery?.status || 'not_run')" />
          </header>
          <ol
            v-if="selectedDelivery"
            class="delivery-rail"
            aria-label="Delivery stages"
          >
            <li
              v-for="(stage, index) in deliveryStages"
              :key="stage.label"
            >
              <span>{{ String(index + 1).padStart(2, "0") }}</span>
              <div><strong>{{ stage.label }}</strong><ResultBadge :result="stage.status" :label="devopsStatusLabel(stage.status)" /><small>{{ stage.detail === 'NOT RUN' ? '尚未运行' : stage.detail }}</small></div>
            </li>
          </ol>
          <div
            v-if="selectedDelivery"
            class="delivery-identities"
          >
            <HashValue
              label="Base revision"
              :value="selectedDelivery.base_revision"
            />
            <HashValue
              label="Commit SHA"
              :value="selectedDelivery.commit_sha"
            />
            <HashValue
              label="Merged commit"
              :value="selectedDelivery.merged_commit_sha"
            />
            <HashValue
              label="Target revision"
              :value="selectedDelivery.target_revision"
            />
            <HashValue
              label="Rollout revision"
              :value="selectedDelivery.rollout_revision"
            />
            <UButton
              v-if="selectedPullRequestURL"
              color="neutral"
              variant="outline"
              icon="i-lucide-external-link"
              :label="`打开 GitHub PR #${selectedDelivery.pull_request_number}`"
              :href="selectedPullRequestURL"
              target="_blank"
              rel="noopener noreferrer"
            />
          </div>
          <p
            v-else
            class="empty-copy"
          >
            Delivery 尚未运行；当前 DevOps 投影没有与该 Incident 绑定的交付记录。
          </p>
        </section>

        <section
          id="verification"
          class="detail-section verification-section"
          aria-labelledby="verification-matrix-heading"
        >
          <header>
            <div>
              <span>当前 Evidence 验证</span><h3 id="verification-matrix-heading">
                Verification Matrix
              </h3>
            </div><ResultBadge :result="detailExecution?.verification?.status || 'not_run'" :label="devopsStatusLabel(detailExecution?.verification?.status || 'not_run')" />
          </header>
          <UAlert
            v-if="detailExecution && !detailExecution.verification"
            color="warning"
            variant="soft"
            icon="i-lucide-clock-3"
            title="尚无当前效果后观测"
            description="Execution 状态不能替代 Verification；当前保持尚未运行。"
          />
          <div
            v-else-if="detailExecution?.verification"
            class="verification-matrix"
            data-testid="devops-verification-matrix"
          >
            <dl>
              <div><dt>Observation</dt><dd><code>{{ detailExecution.verification.id }}</code></dd></div>
              <div><dt>Source</dt><dd>{{ detailExecution.verification.source }}</dd></div>
              <div><dt>状态</dt><dd><ResultBadge :result="detailExecution.verification.status" :label="devopsStatusLabel(detailExecution.verification.status)" /></dd></div>
              <div><dt>Observed UTC</dt><dd>{{ formatUTC(detailExecution.verification.observed_at) }}</dd></div>
              <div><dt>Summary</dt><dd>{{ detailExecution.verification.summary }}</dd></div>
            </dl>
            <HashValue
              label="Evidence content hash"
              :value="detailExecution.verification.content_hash"
            />
            <JSONSnapshot
              title="Provider identity"
              :value="detailExecution.verification.provider_identity"
            />
            <JSONSnapshot
              title="Current Evidence"
              :value="detailExecution.verification.evidence"
            />
          </div>
          <p
            v-else
            class="empty-copy"
          >
            Verification 尚未运行；当前没有 Execution。
          </p>
        </section>
      </template>
    </main>

    <main
      v-else
      class="identity-view"
      data-testid="devops-identity-view"
    >
      <section
        class="baseline-hero"
        aria-labelledby="baseline-heading"
      >
        <header class="section-heading">
          <div>
            <span>当前部署事实</span><h2 id="baseline-heading">DeploymentBaseline</h2>
            <p>当前生效 Baseline 是交付身份主事实；历史版本仅用于对比与追溯。</p>
          </div>
          <ResultBadge :result="activeBaseline?.status || 'not_run'" :label="activeBaseline ? '已生效' : '尚未运行'" />
        </header>
        <div v-if="activeBaseline" class="baseline-summary">
          <div class="baseline-target">
            <span class="baseline-icon"><UIcon name="i-lucide-box" /></span>
            <div><small>当前 Target</small><strong>{{ activeBaseline.namespace }}/{{ activeBaseline.workload_name }}</strong><p>{{ activeBaseline.cluster }} · {{ activeBaseline.environment }} · {{ activeBaseline.workload_kind }}</p></div>
          </div>
          <ol class="identity-chain" aria-label="当前交付身份链">
            <li><span><UIcon name="i-lucide-code-xml" /></span><div><small>源码</small><strong>{{ compactIdentity(activeBaseline.source_revision, 7) }}</strong></div></li>
            <li><span><UIcon name="i-lucide-package" /></span><div><small>镜像</small><strong>{{ compactIdentity(activeBaseline.image_digest, 7) }}</strong></div></li>
            <li><span><UIcon name="i-lucide-git-branch" /></span><div><small>GitOps</small><strong>{{ compactIdentity(activeBaseline.gitops_revision, 7) }}</strong></div></li>
            <li><span><UIcon name="i-lucide-scan-line" /></span><div><small>集群观测</small><strong>row v{{ activeBaseline.row_version }}</strong></div></li>
            <li><span><UIcon name="i-lucide-badge-check" /></span><div><small>验证基线</small><strong>{{ formatUTC(activeBaseline.verified_at) }}</strong></div></li>
          </ol>
          <WorkspaceTechnicalDetails
            title="完整 Baseline 身份"
            description="Source、Image、GitOps、Configuration 与 Verification 精确身份"
            :fields="baselineTechnicalFields(activeBaseline)"
          />
        </div>
        <WorkspaceState
          v-else
          kind="empty"
          title="尚无已验证的 Deployment Baseline"
          description="当前没有可证明的生效 Baseline；不会从 Delivery 或 Execution 状态推断。"
        />
      </section>

      <section
        class="identity-section"
        aria-labelledby="candidate-heading"
      >
        <header class="section-heading">
          <div>
            <span>已观测变更</span><h2 id="candidate-heading">ChangeCandidate</h2>
            <p>Candidate 只说明已观察到变更身份；Incident 所有的变更回到 Incident 审批。</p>
          </div>
          <UBadge color="neutral" variant="soft" :label="`${candidates.length} 项`" />
        </header>
        <ul v-if="candidates.length" class="identity-list candidate-list">
          <li v-for="candidate in candidates" :key="candidate.id">
            <div class="identity-list-main">
              <span class="identity-list-icon"><UIcon name="i-lucide-file-diff" /></span>
              <div><strong>{{ candidate.repository || candidate.source_type }}</strong><p>{{ candidate.category }} · {{ candidate.target_path || "未记录 target path" }}</p><small>{{ formatUTC(candidate.change_time) }}</small></div>
              <UBadge color="warning" variant="soft" label="Incident 所有" />
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-arrow-up-right"
                label="前往 Incident"
                :to="incidentStageHref(candidate.incident_id, 'approval')"
              />
            </div>
            <WorkspaceTechnicalDetails
              title="Candidate 技术身份"
              description="完整 revision、artifact 与 Evidence hash"
              :fields="candidateTechnicalFields(candidate)"
            />
          </li>
        </ul>
        <p v-else class="inline-empty">当前没有持久化 ChangeCandidate；没有待关联的观测变更。</p>
      </section>

      <section
        class="identity-section"
        aria-labelledby="delivery-heading"
      >
        <header class="section-heading">
          <div>
            <span>从 Source 到部署观测</span><h2 id="delivery-heading">Delivery 投影</h2>
            <p>PR、CI、Merge、Argo 与 Rollout 保持顺序关系，不把中间状态提升为 Verification。</p>
          </div>
          <UBadge color="neutral" variant="soft" :label="`${deliveries.length} 条`" />
        </header>
        <ul v-if="deliveries.length" class="identity-list delivery-list">
          <li v-for="delivery in deliveries" :key="delivery.id">
            <header>
              <div><strong>{{ delivery.repository }}</strong><p>{{ delivery.argo_application || "未投影 Argo Application" }} · 可用副本 {{ delivery.available_replicas }}/{{ delivery.desired_replicas }}</p></div>
              <ResultBadge :result="delivery.status" :label="devopsStatusLabel(delivery.status)" />
            </header>
            <ol class="delivery-chain" aria-label="Delivery 投影阶段">
              <li><span>PR</span><strong>{{ delivery.pull_request_number ? `#${delivery.pull_request_number}` : "尚未运行" }}</strong><ResultBadge :result="delivery.pull_request_state || 'not_run'" :label="devopsStatusLabel(delivery.pull_request_state || 'not_run')" /></li>
              <li><span>CI</span><strong>{{ delivery.ci_status ? devopsStatusLabel(delivery.ci_status) : "尚未运行" }}</strong><ResultBadge :result="delivery.ci_status || 'not_run'" :label="devopsStatusLabel(delivery.ci_status || 'not_run')" /></li>
              <li><span>Merge</span><strong>{{ compactIdentity(delivery.merged_commit_sha, 7) }}</strong><ResultBadge :result="delivery.merged_commit_sha ? 'observed' : 'not_run'" :label="devopsStatusLabel(delivery.merged_commit_sha ? 'observed' : 'not_run')" /></li>
              <li><span>Argo</span><strong>{{ delivery.argo_sync_status ? devopsStatusLabel(delivery.argo_sync_status) : "尚未运行" }}</strong><ResultBadge :result="delivery.argo_operation_phase || 'not_run'" :label="devopsStatusLabel(delivery.argo_operation_phase || 'not_run')" /></li>
              <li><span>Rollout</span><strong>{{ delivery.available_replicas }}/{{ delivery.desired_replicas }}</strong><ResultBadge :result="delivery.status || 'not_run'" :label="devopsStatusLabel(delivery.status || 'not_run')" /></li>
            </ol>
            <div class="identity-actions">
              <UButton
                v-if="deliveryPullRequestURL(delivery)"
                color="neutral"
                variant="outline"
                icon="i-lucide-external-link"
                :label="`打开 GitHub PR #${delivery.pull_request_number}`"
                :href="deliveryPullRequestURL(delivery)"
                target="_blank"
                rel="noopener noreferrer"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-route"
                label="Incident Delivery"
                :to="incidentStageHref(delivery.incident_id, 'delivery')"
              />
              <CopyFeedbackButton
                :value="delivery.merged_commit_sha || delivery.commit_sha"
                label="复制交付 Commit"
                success-label="交付 Commit 已复制"
              />
            </div>
          </li>
        </ul>
        <p v-else class="inline-empty">GitHub/Argo Delivery 分支尚未运行；当前没有可展示的交付链。</p>
      </section>

      <section class="identity-section" aria-labelledby="history-heading">
        <header class="section-heading">
          <div><span>部署历史</span><h2 id="history-heading">Baseline 历史与 Diff</h2><p>选择历史版本，与当前生效 Baseline 做真实字段对比。</p></div>
          <UBadge color="neutral" variant="soft" :label="`${historicalBaselines.length} 个历史版本`" />
        </header>
        <WorkspaceDenseList
          :items="historicalBaselines"
          :item-key="(item) => item.id"
          label="Deployment Baseline 历史"
          :selected-key="requestedBaselineID"
          empty="当前没有历史 Baseline。"
          @select="(item) => selectBaseline(item)"
        >
          <template #leading><span class="queue-icon"><UIcon name="i-lucide-history" /></span></template>
          <template #title="{ item }">{{ baselineTarget(item) }}</template>
          <template #description="{ item }">{{ item.repository }} · {{ compactIdentity(item.source_revision, 9) }}</template>
          <template #meta="{ item }">{{ formatUTC(item.verified_at) }}</template>
          <template #trailing="{ item }"><ResultBadge :result="item.status" :label="devopsStatusLabel(item.status)" /></template>
        </WorkspaceDenseList>
        <div v-if="comparedBaseline" class="baseline-diff" role="status" aria-live="polite">
          <header><div><span>对比 Baseline</span><strong>{{ baselineTarget(comparedBaseline) }}</strong></div><UButton color="neutral" variant="ghost" icon="i-lucide-x" label="清除对比" @click="router.replace({ path: route.path, query: { ...route.query, baseline: undefined } })" /></header>
          <dl v-if="baselineDifferences.length">
            <div v-for="difference in baselineDifferences" :key="difference.label"><dt>{{ difference.label }}</dt><dd><span>历史</span><code>{{ compactIdentity(difference.compared, 10) }}</code></dd><dd><span>当前生效</span><code>{{ compactIdentity(difference.active, 10) }}</code></dd></div>
          </dl>
          <p v-else>所选 Baseline 与当前生效 Baseline 的核心身份字段一致。</p>
          <WorkspaceTechnicalDetails title="完整对比身份" description="展开核对历史 Baseline 完整值" :fields="baselineTechnicalFields(comparedBaseline)" />
        </div>
      </section>
    </main>

    <WorkspaceInspector
      :open="Boolean(inspectorID)"
      title="DevOps Inspector"
      description="ExactIdentity、Authority 与当前执行链的只读摘要。"
      :target-state="inspectorTargetState"
      target-description="所选查询未匹配当前 DevOps 投影；不会静默选择第一行。"
      :trigger="inspectorTrigger"
      @update:open="handleInspectorOpenChange"
    >
      <div
        v-if="inspectorSubject"
        class="devops-inspector"
        data-testid="devops-inspector"
      >
        <header>
          <div><span>{{ isOperationPlan(inspectorSubject) ? "Operation Plan" : "Action Card" }}</span><h3>{{ subjectType(inspectorSubject) }}</h3><code translate="no">{{ inspectorSubject.id }}</code></div>
          <ResultBadge :result="inspectorSubject.status" :label="devopsStatusLabel(inspectorSubject.status)" />
        </header>
        <UAlert
          :color="inspectorOwnership.kind === 'incident' ? 'warning' : inspectorOwnership.kind === 'non_incident' ? 'info' : 'error'"
          variant="soft"
          :icon="inspectorOwnership.kind === 'incident' ? 'i-lucide-route' : inspectorOwnership.kind === 'non_incident' ? 'i-lucide-shield-check' : 'i-lucide-shield-x'"
          :title="ownershipLabel(inspectorOwnership)"
          :description="inspectorOwnership.reason"
        />
        <dl class="inspector-facts">
          <div><dt>Authority</dt><dd>{{ inspectorSubject.authority }}</dd></div>
          <div><dt>Expires UTC</dt><dd>{{ formatUTC(inspectorSubject.expires_at) }}</dd></div>
          <div><dt>Execution</dt><dd>{{ devopsStatusLabel(inspectorExecution?.status || "not_run") }}</dd></div>
          <div><dt>Delivery</dt><dd>{{ devopsStatusLabel(inspectorDelivery?.status || "not_run") }}</dd></div>
          <div><dt>Verification</dt><dd>{{ devopsStatusLabel(inspectorExecution?.verification?.status || "not_run") }}</dd></div>
        </dl>
        <HashValue
          label="Exact content hash"
          :value="inspectorSubject.content_hash"
        />
        <HashValue
          v-if="inspectorSubject.authorization"
          label="Authorized hash"
          :value="inspectorSubject.authorization.authorized_content_hash"
        />
        <nav
          v-if="inspectorOwnership.incidentID"
          class="inspector-stage-links"
          aria-label="Incident stage links"
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-file-key-2"
            label="审批"
            :to="incidentStageHref(inspectorOwnership.incidentID, 'approval')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-git-pull-request-arrow"
            label="交付"
            :to="incidentStageHref(inspectorOwnership.incidentID, 'delivery')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-shield-check"
            label="验证"
            :to="incidentStageHref(inspectorOwnership.incidentID, 'verification')"
          />
        </nav>
      </div>
      <template #footer>
        <UButton
          v-if="inspectorSubject"
          color="primary"
          icon="i-lucide-maximize-2"
          label="打开完整技术详情"
          @click="enterFullDetail"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-x"
          label="关闭"
          @click="closeInspector"
        />
      </template>
    </WorkspaceInspector>

    <IncidentCommandConfirmation
      :open="Boolean(confirmationMode)"
      :title="confirmationTitle"
      :description="confirmationDescription"
      :target="confirmationTarget"
      :effect="confirmationEffect"
      :authority="confirmationAuthority"
      :version="confirmationVersion"
      :exact-hash="confirmationHash"
      :recovery="confirmationRecovery"
      :confirm-label="confirmationMode === 'authorize' ? '授权精确 Hash' : confirmationMode === 'execute' ? '排队执行' : '创建精确 Plan'"
      :reason-required="confirmationMode === 'authorize'"
      :pending="confirmationPending"
      :severity="confirmationMode === 'execute' ? 'error' : 'warning'"
      @update:open="(open) => { if (!open) confirmationMode = '' }"
      @confirm="confirmCommand"
    />
  </WorkspacePageFrame>
</template>

<style scoped>
.devops-workspace { display: grid; width: 100%; min-width: 0; gap: var(--co-space-4); }
.header-facts { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-4); color: var(--co-text-secondary); font-size: 11px; }
.header-facts span { display: inline-flex; align-items: baseline; gap: var(--co-space-1); }
.header-facts strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 16px; font-variant-numeric: tabular-nums; }
.header-facts .header-facts__healthy { align-items: center; color: var(--co-status-success-fg); }
.header-facts__healthy svg { width: 14px; height: 14px; }
.provider-strip, .freeze-section, .scenario-strip, .detail-section, .identity-section, .baseline-hero { min-width: 0; overflow: hidden; border: 1px solid transparent; border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.provider-strip { padding: var(--co-space-3); }
.provider-strip > header, .queue-section > header, .freeze-section > header, .identity-section > header, .detail-heading, .detail-section > header, .devops-inspector > header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.provider-strip h2, .provider-strip p, .queue-section h2, .queue-section p, .freeze-section h2, .freeze-section p, .identity-section h2, .detail-heading h2, .detail-section h3, .devops-inspector h3 { margin: 0; }
.provider-strip h2, .queue-section h2, .freeze-section h2, .identity-section h2 { font-size: 15px; }
.provider-strip header p, .queue-section header p, .freeze-section header p { margin-top: 2px; color: var(--co-text-muted); font-size: 11px; }
.provider-strip time, .freeze-section time, .audit-timeline time { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; font-variant-numeric: tabular-nums; }
.provider-strip ul { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: var(--co-space-3) 0 0; padding: 0; border-top: 1px solid var(--co-border-default); list-style: none; }
.provider-strip li { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) auto; gap: var(--co-space-2); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.provider-strip li + li { border-left: 1px solid var(--co-border-default); }
.provider-strip li > div { display: grid; min-width: 0; }
.provider-strip li span, .provider-strip li p { color: var(--co-text-muted); font-size: 10px; }
.provider-strip li p, .provider-strip li code { grid-column: 1 / -1; min-width: 0; margin: 0; overflow-wrap: anywhere; }
.provider-strip li code { color: var(--co-text-secondary); font-size: 10px; }
.provider-popover { display: grid; width: min(420px, calc(100vw - 40px)); gap: var(--co-space-3); padding: var(--co-space-3); }
.provider-popover > header { display: flex; justify-content: space-between; gap: var(--co-space-3); }
.provider-popover > header span, .provider-popover small { color: var(--co-text-muted); font-size: 10px; }
.provider-popover ul { display: grid; gap: var(--co-space-1); margin: 0; padding: 0; list-style: none; }
.provider-popover li { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2); border-block: 1px solid var(--co-border-subtle); }
.provider-popover li > div { display: grid; min-width: 0; gap: 2px; }
.provider-popover small { overflow-wrap: anywhere; }
.provider-dot { width: 8px; height: 8px; border-radius: var(--co-radius-pill); background: var(--co-status-neutral-fg); }
.provider-dot--success { background: var(--co-status-success-fg); }
.provider-dot--warning { background: var(--co-status-warning-fg); }
.error-identities { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.toolbar-summary { display: grid; min-width: 0; gap: 2px; }
.toolbar-summary span { color: var(--co-text-muted); font-size: 10px; }
.devops-view-switcher { display: flex; min-width: 0; min-height: 52px; align-items: center; gap: var(--co-space-4); padding: var(--co-space-2) 0 var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.devops-view-switcher .toolbar-summary { flex: 1 1 auto; }
.devops-view-switcher > :deep(button:last-child) { margin-left: auto; }
.workspace-loading { display: grid; gap: 1px; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); }
.loading-row { height: var(--co-table-row-height); }
.operations-index, .operation-detail, .identity-view, .devops-inspector { display: grid; min-width: 0; gap: var(--co-space-4); }
.attention-strip { display: grid; min-width: 0; grid-template-columns: minmax(180px, .7fr) minmax(0, 1.3fr); gap: var(--co-space-4); padding: var(--co-space-2) 0 var(--co-space-4); border-bottom: 1px solid var(--co-border-subtle); }
.attention-strip.is-clear { grid-template-columns: minmax(220px, .7fr) minmax(0, 1.3fr); padding-bottom: var(--co-space-3); }
.attention-strip header { display: grid; align-content: center; min-width: 0; gap: 3px; }
.attention-strip header span, .section-heading > div > span, .baseline-summary small, .identity-list small, .baseline-diff span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.attention-strip h2, .section-heading h2, .baseline-hero h2 { margin: 0; font-size: 16px; }
.attention-strip header p, .section-heading p, .baseline-hero header p { margin: 3px 0 0; color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.attention-strip dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; }
.attention-strip dl div { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-2) var(--co-space-3); border-left: 1px solid var(--co-border-subtle); }
.attention-strip dt { color: var(--co-text-muted); font-size: 10px; }
.attention-strip dd { margin: 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 20px; font-variant-numeric: tabular-nums; }
.attention-strip small { color: var(--co-text-muted); font-size: 10px; overflow-wrap: anywhere; }
.attention-strip .has-attention dd, .attention-strip .has-attention dt { color: var(--co-status-warning-fg); }
.attention-strip .has-critical dd, .attention-strip .has-critical dt { color: var(--co-status-critical-fg); }
.attention-strip .is-running dd, .attention-strip .is-running dt { color: var(--co-status-info-fg); }
.attention-clear { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3) var(--co-space-4); border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-control); background: color-mix(in srgb, var(--co-status-success-bg) 52%, var(--co-bg-surface)); }
.attention-clear > svg { width: 20px; height: 20px; flex: 0 0 auto; color: var(--co-status-success-fg); }
.attention-clear > div { display: grid; min-width: 0; gap: 2px; }
.attention-clear strong { color: var(--co-text-secondary); font-size: 12px; }
.attention-clear span { color: var(--co-text-muted); font-size: 10px; }
.causal-section { min-width: 0; padding: var(--co-space-2) 0 var(--co-space-4); border-bottom: 1px solid var(--co-border-subtle); }
.section-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.section-heading > div { min-width: 0; }
.section-heading > p { max-width: 420px; text-align: right; }
.compact-heading { padding: var(--co-space-3) var(--co-space-4); }
.causal-chain { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); margin: var(--co-space-4) 0 0; padding: 0; list-style: none; }
.causal-chain li { position: relative; display: grid; min-width: 0; justify-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-2); text-align: center; }
.causal-chain li:not(:last-child)::after { position: absolute; top: 17px; right: -5px; width: 10px; border-top: 1px solid var(--co-border-strong); content: ""; }
.causal-chain li > span { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-floating); }
.causal-chain li > div { display: grid; min-width: 0; gap: 2px; }
.causal-chain small, .causal-chain em { color: var(--co-text-muted); font-size: 10px; font-style: normal; overflow-wrap: anywhere; }
.causal-chain strong { color: var(--co-text-primary); font-size: 11px; }
.queue-icon, .freeze-icon, .identity-list-icon { display: grid; width: 30px; height: 30px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-floating); }
.freeze-icon { color: var(--co-status-warning-fg); }
.scenario-strip { display: grid; grid-template-columns: minmax(240px, .8fr) minmax(420px, 1.5fr) auto; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3); }
.scenario-strip > div { min-width: 0; }
.scenario-strip > div > span, .detail-heading > div > span, .detail-section > header span, .identity-section > header span, .devops-inspector header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scenario-strip h2 { margin: 2px 0 0; font-size: 15px; }
.scenario-strip p { margin: 2px 0 0; color: var(--co-text-secondary); font-size: 11px; }
.scenario-strip dl { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; gap: 0; }
.scenario-strip dl div { min-width: 0; padding: var(--co-space-2); }
.scenario-strip dl div + div { border-left: 1px solid var(--co-border-subtle); }
.scenario-strip dt, .fact-grid dt, .inspector-facts dt, .verification-matrix dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scenario-strip dd, .fact-grid dd, .inspector-facts dd, .verification-matrix dd { min-width: 0; margin: 2px 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.queue-section { min-width: 0; overflow: visible; }
.queue-section > header { padding: 0 0 var(--co-space-3); }
.queue-row-description { display: grid; min-width: 0; gap: 2px; }
.queue-row-description > span { color: var(--co-text-secondary); }
.queue-row-description small { min-width: 0; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.queue-row-meta { display: grid; max-width: 210px; justify-items: end; gap: 2px; font-family: var(--co-font-sans); text-align: right; }
.queue-row-meta strong { color: var(--co-text-secondary); font-size: 10px; }
.queue-row-meta small { color: var(--co-text-muted); font-size: 9px; line-height: 1.35; }
.queue-status-stack { display: grid; min-width: 92px; justify-items: end; gap: 3px; }
.queue-status-stack > span { color: var(--co-text-muted); font-size: 10px; }
.queue-status-stack > small { color: var(--co-status-success-fg); font-size: 9px; }
.freeze-section > header, .identity-section > header { padding: var(--co-space-3); }
.identity-section :deep(.workspace-dense-list) { padding: 0 var(--co-space-3) var(--co-space-3); }
.devops-table-stack { display: grid; min-width: 0; justify-items: start; gap: 3px; }
.devops-table-stack strong { color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.devops-table-stack span, .muted-copy { color: var(--co-text-muted); font-size: 11px; }
.devops-table-stack code, .dense-hash, .identity-hash { min-width: 0; max-width: 100%; overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.freeze-section ul { margin: 0; padding: 0; list-style: none; }
.freeze-section li { display: grid; min-width: 0; min-height: 52px; grid-template-columns: minmax(240px, .7fr) auto minmax(260px, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.freeze-section li > div { display: grid; min-width: 0; }
.freeze-section li p { color: var(--co-text-secondary); overflow-wrap: anywhere; }
.empty-copy { margin: 0; padding: var(--co-space-4); color: var(--co-text-muted); }
.detail-heading { padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.detail-heading > div { display: grid; min-width: 0; justify-items: start; gap: 3px; }
.detail-heading > div:last-child { display: flex; align-items: center; }
.detail-heading h2 { font-size: 18px; overflow-wrap: anywhere; }
.detail-heading code { color: var(--co-text-secondary); overflow-wrap: anywhere; }
.incident-stage-links, .inspector-stage-links, .technical-links, .execution-selector, .subject-actions { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.detail-section > header { padding: var(--co-space-3) var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.detail-section > header > div { min-width: 0; }
.detail-section h3 { margin-top: 2px; font-size: 16px; overflow-wrap: anywhere; }
.hash-grid, .delivery-identities { display: grid; min-width: 0; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 var(--co-space-4); padding: 0 var(--co-space-4); }
.fact-grid, .inspector-facts, .verification-matrix dl { display: grid; min-width: 0; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; padding: var(--co-space-4); }
.fact-grid div, .inspector-facts div, .verification-matrix dl div { min-width: 0; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.subject-actions, .execution-selector, .technical-links { padding: var(--co-space-3) var(--co-space-4); border-top: 1px solid var(--co-border-default); }
.execution-summary { display: grid; min-width: 0; }
.audit-timeline { display: grid; min-width: 0; margin: 0; padding: var(--co-space-4); list-style: none; }
.audit-timeline li { display: grid; min-width: 0; grid-template-columns: 34px minmax(0, 1fr); gap: var(--co-space-3); padding-bottom: var(--co-space-4); }
.audit-timeline li > span { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-pill); font-family: var(--co-font-mono); font-size: 10px; }
.audit-timeline li > div { display: grid; min-width: 0; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.audit-timeline li header { display: flex; min-width: 0; justify-content: space-between; gap: var(--co-space-3); }
.delivery-rail { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); min-width: 0; margin: 0; padding: 0; list-style: none; }
.delivery-rail li { display: grid; min-width: 0; grid-template-columns: 32px minmax(0, 1fr); gap: var(--co-space-2); padding: var(--co-space-4) var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.delivery-rail li + li { border-left: 1px solid var(--co-border-default); }
.delivery-rail li > span { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-pill); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.delivery-rail li > div { display: grid; min-width: 0; justify-items: start; gap: var(--co-space-2); }
.delivery-rail small { color: var(--co-text-muted); overflow-wrap: anywhere; }
.delivery-identities { align-items: end; padding-block: var(--co-space-3); }
.verification-matrix { display: grid; min-width: 0; gap: var(--co-space-3); }
.identity-section { overflow-x: auto; }
.baseline-hero { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-4); }
.baseline-hero > header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.baseline-summary { display: grid; min-width: 0; gap: var(--co-space-4); }
.baseline-target { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-floating); }
.baseline-target > div { display: grid; min-width: 0; gap: 2px; }
.baseline-target strong { font-size: 18px; overflow-wrap: anywhere; }
.baseline-target p { margin: 0; color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.baseline-icon { display: grid; width: 42px; height: 42px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-control); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.identity-chain, .delivery-chain { display: grid; min-width: 0; margin: 0; padding: 0; list-style: none; }
.identity-chain { grid-template-columns: repeat(5, minmax(0, 1fr)); }
.identity-chain li { position: relative; display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: var(--co-space-2); padding: var(--co-space-2); border-block: 1px solid var(--co-border-subtle); }
.identity-chain li + li { border-left: 1px solid var(--co-border-subtle); }
.identity-chain li > span { display: grid; width: 28px; height: 28px; place-items: center; border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-active); }
.identity-chain li > div { display: grid; min-width: 0; gap: 2px; }
.identity-chain strong { min-width: 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.identity-list { display: grid; margin: 0; padding: 0; list-style: none; }
.identity-list > li { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-3) var(--co-space-4); border-top: 1px solid var(--co-border-subtle); }
.identity-list-main, .identity-list > li > header { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.identity-list-main > div, .identity-list > li > header > div { display: grid; min-width: 0; flex: 1 1 auto; gap: 2px; }
.identity-list-main strong, .identity-list > li > header strong { overflow-wrap: anywhere; }
.identity-list-main p, .identity-list > li > header p { margin: 0; color: var(--co-text-secondary); font-size: 11px; overflow-wrap: anywhere; }
.identity-list-main small { font-weight: 400; }
.identity-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); }
.delivery-chain { grid-template-columns: repeat(5, minmax(0, 1fr)); border-block: 1px solid var(--co-border-subtle); }
.delivery-chain li { display: grid; min-width: 0; gap: 3px; padding: var(--co-space-3); border-right: 1px solid var(--co-border-subtle); }
.delivery-chain li:last-child { border-right: 0; }
.delivery-chain span { color: var(--co-text-muted); font-size: 10px; }
.delivery-chain strong { min-width: 0; font-family: var(--co-font-mono); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.baseline-diff { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-floating); }
.baseline-diff > header { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.baseline-diff > header > div { display: grid; min-width: 0; gap: 2px; }
.baseline-diff > header strong { overflow-wrap: anywhere; }
.baseline-diff dl { display: grid; margin: 0; }
.baseline-diff dl > div { display: grid; grid-template-columns: minmax(120px, .5fr) repeat(2, minmax(0, 1fr)); gap: var(--co-space-3); padding: var(--co-space-2) 0; border-top: 1px solid var(--co-border-subtle); }
.baseline-diff dd { display: grid; min-width: 0; gap: 2px; margin: 0; }
.baseline-diff code { color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.baseline-diff p, .inline-empty { margin: 0; padding: var(--co-space-3) var(--co-space-4); color: var(--co-text-muted); font-size: 11px; }
.inline-empty--healthy { display: flex; align-items: center; gap: var(--co-space-2); padding-block: var(--co-space-2); color: var(--co-status-success-fg); }
.inline-empty--healthy svg { width: 15px; height: 15px; }
.devops-inspector > header > div { display: grid; min-width: 0; gap: 2px; }
.devops-inspector header code { min-width: 0; color: var(--co-text-muted); overflow-wrap: anywhere; }
.inspector-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 0; }
.inspector-stage-links { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }

@media (max-width: 1180px) {
  .causal-chain { grid-template-columns: repeat(4, minmax(0, 1fr)); row-gap: var(--co-space-3); }
  .causal-chain li:nth-child(4n)::after { display: none; }
  .identity-chain { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .identity-chain li:nth-child(4) { border-left: 0; }
  .delivery-chain { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .delivery-chain li:nth-child(4) { border-top: 1px solid var(--co-border-subtle); }
}

@media (max-width: 1024px) {
  .queue-row-meta { width: 100%; max-width: none; justify-items: start; text-align: left; }
}

@media (max-width: 900px) {
  .devops-view-switcher { align-items: flex-start; flex-direction: column; }
  .devops-view-switcher > :deep(button:last-child) { margin-left: 0; }
  .attention-strip { grid-template-columns: minmax(0, 1fr); }
  .attention-strip.is-clear { grid-template-columns: minmax(0, 1fr); }
  .attention-strip dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .attention-strip dl div { padding-block: var(--co-space-2); }
  .section-heading { flex-direction: column; }
  .section-heading > p { max-width: none; text-align: left; }
  .causal-chain { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .causal-chain li:nth-child(2n)::after { display: none; }
  .identity-chain, .delivery-chain { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .identity-chain li:nth-child(3), .delivery-chain li:nth-child(3) { border-left: 0; border-top: 1px solid var(--co-border-subtle); }
  .identity-list-main { flex-wrap: wrap; }
  .identity-list-main > :deep(.u-badge) { margin-left: 42px; }
  .baseline-diff dl > div { grid-template-columns: minmax(0, 1fr) repeat(2, minmax(0, 1fr)); }
  .freeze-section li { grid-template-columns: auto minmax(0, 1fr) auto; }
  .freeze-section li p, .freeze-section li time { grid-column: 2 / -1; }
  .hash-grid, .delivery-identities, .fact-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (prefers-reduced-motion: reduce) {
  .devops-workspace :deep(*) { scroll-behavior: auto !important; transition-duration: var(--co-motion-reduced) !important; animation-duration: var(--co-motion-reduced) !important; }
}
</style>
