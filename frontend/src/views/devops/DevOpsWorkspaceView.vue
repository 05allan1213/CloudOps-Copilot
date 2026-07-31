<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import { computed, h, onBeforeUnmount, onMounted, ref, resolveComponent } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { ActionCard, OperationPlan, OperationPlanProposalInput } from "../../api/agent";
import type {
  DeliveryProjection,
  OperationExecution,
  ProviderBranch,
} from "../../api/devops";
import HashValue from "../../components/incidents/HashValue.vue";
import IncidentCommandConfirmation from "../../components/incidents/IncidentCommandConfirmation.vue";
import JSONSnapshot from "../../components/incidents/JSONSnapshot.vue";
import ResultBadge from "../../components/incidents/ResultBadge.vue";
import ContextToolbar from "../../components/workspace/ContextToolbar.vue";
import DenseDataTable, { type DenseTableColumn } from "../../components/workspace/DenseDataTable.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector from "../../components/workspace/WorkspaceInspector.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
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

interface CandidateRow extends Record<string, unknown> {
  id: string;
  source: string;
  revision: string;
  artifact: string;
  evidenceHash: string;
  observedAt: string;
  incidentID: string;
}

interface BaselineRow extends Record<string, unknown> {
  id: string;
  target: string;
  sourceRevision: string;
  imageDigest: string;
  gitOpsRevision: string;
  verificationHash: string;
  status: string;
  verifiedAt: string;
}

interface DeliveryRow extends Record<string, unknown> {
  id: string;
  repository: string;
  pullRequest: string;
  pullRequestURL: string;
  commit: string;
  ci: string;
  argo: string;
  rollout: string;
  status: string;
  incidentID: string;
}

const route = useRoute();
const router = useRouter();
const store = useDevOpsWorkspaceStore();
const UBadge = resolveComponent("UBadge");
const UButton = resolveComponent("UButton");
const queueTable = ref<{
  getRowElement: (rowID: string) => HTMLElement | null;
  getScrollElement: () => HTMLElement | null;
} | null>(null);
const {
  selectedID: inspectorID,
  triggerElement: inspectorTrigger,
  open: openInspector,
  close: closeInspector,
  openFull: openFullDetail,
} = useWorkspaceInspector({
  selectedKey: "selected",
  scrollElement: () => queueTable.value?.getScrollElement() ?? null,
  resolveTrigger: (subjectID) => queueTable.value?.getRowElement(subjectID) ?? null,
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
const observedCount = computed(() => executions.value.filter((item) => Boolean(item.verification)).length);
const verifiedCount = computed(() => executions.value.filter((item) => item.verification?.status === "passed").length);
const queueRows = computed<DevOpsQueueRow[]>(() => subjects.value.map((subject) => {
  const execution = executions.value.find((item) => item.subject_id === subject.id) ?? null;
  return {
    id: subject.id,
    subject,
    type: subjectType(subject),
    kind: isOperationPlan(subject) ? "Operation Plan" : "Action Card",
    authority: subject.authority,
    status: subject.status,
    contentHash: subject.content_hash,
    createdAt: subject.created_at,
    ownership: classifyDevOpsSubject(subject, executions.value, store.investigations),
    execution,
  };
}));

const queueColumns = computed<DenseTableColumn<DevOpsQueueRow>[]>(() => [
  {
    id: "identity",
    accessorKey: "type",
    label: "Subject",
    header: "Subject / exact identity",
    size: 300,
    cell: ({ row }) => h("div", { class: "devops-table-stack" }, [
      h("strong", { "data-testid": "devops-row-type" }, row.original.type),
      h("span", {}, row.original.kind),
      h("code", { translate: "no" }, row.original.id),
    ]),
  },
  {
    id: "authority",
    accessorKey: "authority",
    label: "Authority",
    header: "Authority / ownership",
    size: 215,
    cell: ({ row }) => h("div", { class: "devops-table-stack" }, [
      h("strong", {}, row.original.authority),
      h(UBadge, {
        color: ownershipColor(row.original.ownership.kind),
        variant: "soft",
        label: ownershipLabel(row.original.ownership),
      }),
    ]),
  },
  {
    id: "status",
    accessorKey: "status",
    label: "Subject 状态",
    header: "Subject 状态",
    size: 150,
    cell: ({ row }) => h(ResultBadge, { result: row.original.status }),
  },
  {
    id: "execution",
    accessorKey: "execution",
    label: "Execution / Verify",
    header: "Execution / Verify",
    size: 210,
    cell: ({ row }) => row.original.execution
      ? h("div", { class: "devops-table-stack" }, [
          h(ResultBadge, { result: row.original.execution.status }),
          h("span", {}, `Verify ${row.original.execution.verification?.status ?? "NOT RUN"}`),
        ])
      : h("span", { class: "muted-copy" }, "Execution NOT RUN"),
  },
  {
    id: "hash",
    accessorKey: "contentHash",
    label: "Exact hash",
    header: "Exact hash",
    size: 220,
    optional: true,
    cell: ({ row }) => h("code", { class: "dense-hash", translate: "no" }, compactIdentity(row.original.contentHash, 12)),
  },
  {
    id: "created",
    accessorKey: "createdAt",
    label: "Created UTC",
    header: "Created UTC",
    size: 190,
    optional: true,
    cell: ({ row }) => h("time", { datetime: row.original.createdAt }, formatUTC(row.original.createdAt)),
  },
]);

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
  if (!scenarioDeployment.value.resource_version) return "Kubernetes projection 缺少 resourceVersion。";
  if (!scenarioInvestigation.value) return "尚无同一 Scenario 的已完成 Agent Investigation 与 Evidence。";
  if (scenarioFreeze.value?.enabled) return "当前 target 已进入 change freeze。";
  if (scenarioPlan.value) return `该 Scenario 已有 ${scenarioPlan.value.status} Operation Plan。`;
  return "";
});

const selectedDelivery = computed(() => deliveryFor(detailOwnership.value.incidentID));
const selectedPullRequestURL = computed(() => safeExternalURL(selectedDelivery.value?.pull_request_url));
const deliveryStages = computed(() => deliveryStageRows(selectedDelivery.value));
const technicalLinks = computed(() => (detailExecution.value?.links ?? []).filter((link) => (
  link.kind !== "incident" && link.kind !== "verification" && link.href.startsWith("/")
)));

const candidateRows = computed<CandidateRow[]>(() => (workspace.value?.change_candidates ?? []).map((item) => ({
  id: item.id,
  source: `${item.category} · ${item.repository || item.source_type}`,
  revision: item.commit_sha || item.gitops_revision,
  artifact: item.image_digest || item.gitops_revision,
  evidenceHash: item.content_hash,
  observedAt: item.change_time,
  incidentID: item.incident_id,
})));
const baselineRows = computed<BaselineRow[]>(() => (workspace.value?.deployment_baselines ?? []).map((item) => ({
  id: item.id,
  target: `${item.cluster}/${item.namespace}/${item.workload_kind}/${item.workload_name}`,
  sourceRevision: item.source_revision,
  imageDigest: item.image_digest,
  gitOpsRevision: item.gitops_revision,
  verificationHash: item.verification_hash,
  status: item.status,
  verifiedAt: item.verified_at,
})));
const deliveryRows = computed<DeliveryRow[]>(() => (workspace.value?.deliveries ?? []).map((item) => ({
  id: item.id,
  repository: item.repository,
  pullRequest: item.pull_request_number ? `PR #${item.pull_request_number} · ${item.pull_request_state}` : "PR NOT RUN",
  pullRequestURL: safeExternalURL(item.pull_request_url),
  commit: item.merged_commit_sha || item.commit_sha,
  ci: item.ci_status || "NOT RUN",
  argo: [item.argo_sync_status, item.argo_operation_phase, item.argo_health_status].filter(Boolean).join(" / ") || "NOT RUN",
  rollout: `${item.available_replicas}/${item.desired_replicas} available`,
  status: item.status,
  incidentID: item.incident_id,
})));

const candidateColumns: TableColumn<CandidateRow>[] = [
  { accessorKey: "source", header: "Change / source" },
  { accessorKey: "revision", header: "Source revision", cell: ({ row }) => hashCell(row.original.revision) },
  { accessorKey: "artifact", header: "GitOps / image", cell: ({ row }) => hashCell(row.original.artifact) },
  { accessorKey: "evidenceHash", header: "Evidence hash", cell: ({ row }) => hashCell(row.original.evidenceHash) },
  { accessorKey: "observedAt", header: "Observed UTC", cell: ({ row }) => h("time", {}, formatUTC(row.original.observedAt)) },
  { id: "incident", header: "Incident", cell: ({ row }) => incidentLinkCell(row.original.incidentID, "approval") },
];
const baselineColumns: TableColumn<BaselineRow>[] = [
  { accessorKey: "target", header: "Target" },
  { accessorKey: "sourceRevision", header: "Source revision", cell: ({ row }) => hashCell(row.original.sourceRevision) },
  { accessorKey: "imageDigest", header: "Image digest", cell: ({ row }) => hashCell(row.original.imageDigest) },
  { accessorKey: "gitOpsRevision", header: "GitOps revision", cell: ({ row }) => hashCell(row.original.gitOpsRevision) },
  { accessorKey: "verificationHash", header: "Verification hash", cell: ({ row }) => hashCell(row.original.verificationHash) },
  { accessorKey: "status", header: "Status", cell: ({ row }) => h(ResultBadge, { result: row.original.status }) },
  { accessorKey: "verifiedAt", header: "Verified UTC", cell: ({ row }) => h("time", {}, formatUTC(row.original.verifiedAt)) },
];
const deliveryColumns: TableColumn<DeliveryRow>[] = [
  { accessorKey: "repository", header: "Repository" },
  { accessorKey: "pullRequest", header: "Pull request", cell: ({ row }) => providerLinkCell(row.original) },
  { accessorKey: "commit", header: "Exact commit", cell: ({ row }) => hashCell(row.original.commit) },
  { accessorKey: "ci", header: "CI", cell: ({ row }) => h(ResultBadge, { result: row.original.ci }) },
  { accessorKey: "argo", header: "Argo observation" },
  { accessorKey: "rollout", header: "Rollout" },
  { accessorKey: "status", header: "Status", cell: ({ row }) => h(ResultBadge, { result: row.original.status }) },
  { id: "incident", header: "Incident", cell: ({ row }) => incidentLinkCell(row.original.incidentID, "delivery") },
];

const tabs = [
  { label: "Operations", value: "operations", icon: "i-lucide-shield-check" },
  { label: "Delivery Identity", value: "identity", icon: "i-lucide-git-commit-horizontal" },
];
const confirmationMode = ref<ConfirmationMode>("");
const confirmationSubject = computed(() => detailSubject.value);
const confirmationTitle = computed(() => {
  if (confirmationMode.value === "authorize") return "Owner review · exact authorization";
  if (confirmationMode.value === "execute") return isOperationPlan(confirmationSubject.value) ? "执行 high-impact Operation Plan" : "执行本地可逆动作";
  return "创建 immutable Scenario Recovery Plan";
});
const confirmationDescription = computed(() => {
  if (confirmationMode.value === "authorize") return "Authorization 只绑定当前 content hash；材料变化后必须重新审查。";
  if (confirmationMode.value === "execute") return "Worker 会再次检查 authority、expiry、exact hash 与 preconditions。";
  return "仅创建持久化 Plan，不授权也不执行 Kubernetes mutation。";
});
const confirmationTarget = computed(() => {
  if (confirmationMode.value === "scenario") return "demo/Deployment/cloudops-scenario-fault";
  return subjectTarget(confirmationSubject.value);
});
const confirmationEffect = computed(() => {
  if (confirmationMode.value === "authorize") return "绑定 Local Owner review 与当前 exact subject。";
  if (confirmationMode.value === "execute") return "按已授权材料排队执行；排队不代表 Provider observed 或 verified。";
  return "创建 replicas=0 的 recovery proposal；不产生 Provider side effect。";
});
const confirmationAuthority = computed(() => {
  if (confirmationMode.value === "scenario") return "Proposal only · not authorized";
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
  ? "删除或拒绝 proposal 不会回滚任何 Provider 状态；执行仍需独立 authorization。"
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
  if (ownership.kind === "incident") return "Incident-owned";
  if (ownership.kind === "non_incident") return "DevOps-owned";
  return "Ownership unknown";
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
    { label: "Draft PR", status: delivery.pull_request_state || "not_run", detail: delivery.pull_request_number ? `PR #${delivery.pull_request_number}` : "NOT RUN" },
    { label: "Required CI", status: delivery.ci_status || "not_run", detail: delivery.ci_status || "NOT RUN" },
    { label: "Human Merge", status: delivery.merged_commit_sha ? "observed" : "not_run", detail: compactIdentity(delivery.merged_commit_sha, 10) },
    { label: "Argo Sync", status: delivery.argo_sync_status || "not_run", detail: [delivery.argo_operation_phase, delivery.argo_health_status].filter(Boolean).join(" / ") || "NOT RUN" },
    { label: "Rollout", status: delivery.status || "not_run", detail: `${delivery.available_replicas}/${delivery.desired_replicas} available` },
  ];
}

function hashCell(value: string) {
  return h("code", { class: "identity-hash", translate: "no", title: value }, compactIdentity(value, 10));
}

function incidentLinkCell(incidentID: string, stage: "approval" | "delivery" | "verification") {
  const to = incidentStageHref(incidentID, stage);
  return to
    ? h(UButton, { color: "neutral", variant: "ghost", size: "xs", icon: "i-lucide-arrow-up-right", label: compactIdentity(incidentID), to })
    : h("span", { class: "muted-copy" }, "未绑定");
}

function providerLinkCell(row: DeliveryRow) {
  if (!row.pullRequestURL) return h("span", {}, row.pullRequest);
  return h(UButton, {
    color: "neutral",
    variant: "link",
    size: "xs",
    icon: "i-lucide-external-link",
    label: row.pullRequest,
    href: row.pullRequestURL,
    target: "_blank",
    rel: "noopener noreferrer",
  });
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
  <section
    class="devops-workspace"
    aria-labelledby="devops-heading"
  >
    <WorkspaceHeader
      heading-id="devops-heading"
      eyebrow="Controlled operations"
      title="DevOps Workspace"
      description="全局队列、非事故 Operation 与不可变交付身份；事故处置回到 Incident 单一生命周期。"
    >
      <template #context>
        <div
          class="header-facts"
          aria-label="DevOps projection 摘要"
        >
          <span><strong>{{ proposedCount }}</strong> 待审查</span>
          <span><strong>{{ authorizedCount }}</strong> 已授权</span>
          <span><strong>{{ store.activeExecutions.length }}</strong> 执行中</span>
          <span><strong>{{ observedCount }}</strong> 已观测</span>
          <span><strong>{{ verifiedCount }}</strong> Verify passed</span>
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

    <section
      v-if="workspace"
      class="provider-strip"
      aria-labelledby="provider-heading"
    >
      <header>
        <div>
          <h2 id="provider-heading">
            Provider branches
          </h2><p>只呈现当前配置与可用性事实。</p>
        </div>
        <time :datetime="workspace.collected_at">{{ formatUTC(workspace.collected_at) }}</time>
      </header>
      <ul>
        <li
          v-for="provider in workspace.providers"
          :key="provider.provider"
        >
          <div><strong>{{ provider.provider }}</strong><span>{{ provider.role }}</span></div>
          <ResultBadge
            :result="providerOutcome(provider)"
            :label="providerOutcome(provider)"
          />
          <p>{{ provider.detail }}</p>
          <code translate="no">{{ provider.configuration_revision_id }}</code>
        </li>
      </ul>
    </section>

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

    <ContextToolbar
      label="DevOps Workspace 视图"
      tabbed
    >
      <template #tabs>
        <UTabs
          :model-value="activeView"
          :items="tabs"
          :content="false"
          color="primary"
          variant="link"
          @update:model-value="setView"
        />
      </template>
      <template #filters>
        <div class="toolbar-summary">
          <strong>{{ activeView === "operations" ? "Authority 与 execution" : "交付与 deployment identity" }}</strong>
          <span>Query：view / subject / operation / selected</span>
        </div>
      </template>
      <template
        v-if="fullDetailRequested"
        #secondary
      >
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-list"
          label="返回全局队列"
          @click="returnToQueue"
        />
      </template>
    </ContextToolbar>

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
      title="当前没有 DevOps projection"
      description="没有加载到 durable operation、identity 或 Provider branch。"
    />

    <main
      v-else-if="activeView === 'operations' && !fullDetailRequested"
      class="operations-index"
    >
      <section
        class="scenario-strip"
        aria-labelledby="scenario-heading"
      >
        <div>
          <span>Global / non-incident proposal</span>
          <h2 id="scenario-heading">
            Scenario Recovery
          </h2>
          <p>{{ scenarioProposalBlocker || "Agent Evidence、resourceVersion 与 freeze precondition 已齐备。" }}</p>
        </div>
        <dl>
          <div><dt>Scenario</dt><dd><code translate="no">{{ scenarioID || "NOT RUN" }}</code></dd></div>
          <div><dt>Replicas</dt><dd>{{ scenarioDeployment?.workload?.ready_replicas ?? 0 }} / {{ scenarioDeployment?.workload?.desired_replicas ?? 0 }} ready</dd></div>
          <div><dt>Evidence</dt><dd>{{ scenarioInvestigation ? `${scenarioInvestigation.evidence_count} citations` : "NOT RUN" }}</dd></div>
          <div><dt>Freeze</dt><dd>{{ scenarioFreeze?.enabled ? "FROZEN" : "OPEN / NOT PROJECTED" }}</dd></div>
        </dl>
        <UButton
          color="warning"
          variant="soft"
          icon="i-lucide-file-plus-2"
          label="创建 exact Recovery Plan"
          :disabled="!canProposeScenarioPlan || Boolean(store.mutatingSubjectID)"
          @click="openConfirmation('scenario')"
        />
      </section>

      <section
        class="queue-section"
        aria-labelledby="queue-heading"
        data-testid="devops-global-queue"
      >
        <header>
          <div>
            <h2 id="queue-heading">
              全局 Authority Queue
            </h2><p>选择行打开只读 Inspector；写操作仅在可恢复的完整详情中出现。</p>
          </div>
          <UBadge
            color="neutral"
            variant="soft"
            :label="`${queueRows.length} subjects`"
          />
        </header>
        <DenseDataTable
          ref="queueTable"
          :rows="queueRows"
          :columns="queueColumns"
          :row-key="(row) => row.id"
          storage-key="devops-authority-queue"
          caption="DevOps 全局与事故关联 authority subjects；选择行打开压缩 Inspector。"
          :critical-column-ids="['identity', 'authority', 'status']"
          :selected-id="inspectorID"
          :copy-value="(row) => `${row.id}\n${row.contentHash}`"
          empty="当前没有持久化 Operation Plan 或 Action Card。"
          @select="selectQueueRow"
        />
      </section>

      <section
        class="freeze-section"
        aria-labelledby="freeze-heading"
      >
        <header>
          <div>
            <h2 id="freeze-heading">
              Change freezes
            </h2><p>本地 freeze truth 与 row version。</p>
          </div><UBadge
            color="neutral"
            variant="soft"
            :label="String(workspace.change_freezes.length)"
          />
        </header>
        <ul v-if="workspace.change_freezes.length">
          <li
            v-for="freeze in workspace.change_freezes"
            :key="`${freeze.target.cluster_id}/${freeze.target.namespace}/${freeze.target.workload_name}`"
          >
            <div><strong>{{ freeze.target.namespace }}/{{ freeze.target.workload_name }}</strong><code>row v{{ freeze.row_version }}</code></div>
            <ResultBadge
              :result="freeze.enabled ? 'warning' : 'success'"
              :label="freeze.enabled ? 'FROZEN' : 'OPEN'"
            />
            <p>{{ freeze.reason || "未记录原因" }}</p>
            <time :datetime="freeze.updated_at">{{ formatUTC(freeze.updated_at) }}</time>
          </li>
        </ul>
        <p
          v-else
          class="empty-copy"
        >
          无本地 freeze 记录。
        </p>
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
        title="DevOps Query 无法恢复目标"
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
            <span>Full detail · {{ detailSubject ? (isOperationPlan(detailSubject) ? "Operation Plan" : "Action Card") : "Execution" }}</span>
            <h2>{{ detailSubject ? subjectType(detailSubject) : detailExecution?.operation_type }}</h2>
            <code translate="no">{{ detailSubject?.id || detailExecution?.subject_id }}</code>
          </div>
          <div>
            <UBadge
              :color="ownershipColor(detailOwnership.kind)"
              variant="soft"
              :label="ownershipLabel(detailOwnership)"
            />
            <ResultBadge :result="detailSubject?.status || detailExecution?.status || 'unknown'" />
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
          title="Ownership 无法证明，写入口保持关闭"
          :description="detailOwnership.reason"
        />

        <nav
          v-if="detailOwnership.incidentID"
          class="incident-stage-links"
          aria-label="Incident lifecycle stages"
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-file-key-2"
            label="Incident Approval"
            :to="incidentStageHref(detailOwnership.incidentID, 'approval')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-git-pull-request-arrow"
            label="Incident Delivery"
            :to="incidentStageHref(detailOwnership.incidentID, 'delivery')"
          />
          <UButton
            color="primary"
            variant="soft"
            icon="i-lucide-shield-check"
            label="Incident Verification"
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
            </div><ResultBadge :result="authorizationCurrent ? 'authorized' : authorizationExpired ? 'expired' : 'not_authorized'" />
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
            <div><dt>Authority</dt><dd>{{ detailSubject?.authority || "未加载 subject" }}</dd></div>
            <div><dt>Configuration Revision</dt><dd><code>{{ isOperationPlan(detailSubject) ? detailSubject.configuration_revision_id : detailExecution?.configuration_revision_id || "run-bound" }}</code></dd></div>
            <div><dt>Subject expires UTC</dt><dd>{{ formatUTC(detailSubject?.expires_at) }}</dd></div>
            <div><dt>Authorized by</dt><dd>{{ detailAuthorization ? `${detailAuthorization.authorized_by} · ${detailAuthorization.reason}` : "NOT AUTHORIZED" }}</dd></div>
            <div><dt>Authorization expires UTC</dt><dd>{{ formatUTC(detailAuthorization?.expires_at) }}</dd></div>
            <div><dt>Risk</dt><dd>{{ detailSubject?.risk || "未加载 subject risk" }}</dd></div>
          </dl>
          <JSONSnapshot
            v-if="detailSubject"
            title="Exact material payload"
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
              label="授权 exact hash"
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
            >当前状态没有可用 DevOps command。</span>
          </div>
        </section>

        <section
          class="detail-section execution-section"
          aria-labelledby="execution-heading"
        >
          <header>
            <div>
              <span>Accepted / dispatched / observed</span><h3 id="execution-heading">
                Execution & audit
              </h3>
            </div><UBadge
              color="neutral"
              variant="soft"
              :label="`${subjectExecutions.length || (detailExecution ? 1 : 0)} attempts`"
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
              :label="`Attempt ${execution.attempt} · ${execution.status}`"
              @click="selectExecution(execution)"
            />
          </nav>
          <div
            v-if="detailExecution"
            class="execution-summary"
          >
            <dl class="fact-grid">
              <div><dt>Execution ID</dt><dd><code>{{ detailExecution.id }}</code></dd></div>
              <div><dt>Status</dt><dd><ResultBadge :result="detailExecution.status" /></dd></div>
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
              尚无 execution audit event。
            </p>
          </div>
          <p
            v-else
            class="empty-copy"
          >
            Execution NOT RUN；没有 accepted、dispatched、observed 或 verified 事实。
          </p>
        </section>

        <section
          class="detail-section delivery-section"
          aria-labelledby="delivery-rail-heading"
        >
          <header>
            <div>
              <span>Linear delivery observation</span><h3 id="delivery-rail-heading">
                Delivery Rail
              </h3>
            </div><ResultBadge :result="selectedDelivery?.status || 'not_run'" />
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
              <div><strong>{{ stage.label }}</strong><ResultBadge :result="stage.status" /><small>{{ stage.detail }}</small></div>
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
            Delivery NOT RUN；当前 DevOps projection 没有与该 Incident 绑定的 delivery。
          </p>
        </section>

        <section
          id="verification"
          class="detail-section verification-section"
          aria-labelledby="verification-matrix-heading"
        >
          <header>
            <div>
              <span>Current Evidence Verify</span><h3 id="verification-matrix-heading">
                Verification Matrix
              </h3>
            </div><ResultBadge :result="detailExecution?.verification?.status || 'not_run'" />
          </header>
          <UAlert
            v-if="detailExecution && !detailExecution.verification"
            color="warning"
            variant="soft"
            icon="i-lucide-clock-3"
            title="尚无 current post-effect observation"
            description="Execution 状态不能替代 Verification；当前保持 NOT RUN。"
          />
          <div
            v-else-if="detailExecution?.verification"
            class="verification-matrix"
            data-testid="devops-verification-matrix"
          >
            <dl>
              <div><dt>Observation</dt><dd><code>{{ detailExecution.verification.id }}</code></dd></div>
              <div><dt>Source</dt><dd>{{ detailExecution.verification.source }}</dd></div>
              <div><dt>Status</dt><dd><ResultBadge :result="detailExecution.verification.status" /></dd></div>
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
            Verification NOT RUN；当前没有 execution。
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
        class="identity-section"
        aria-labelledby="candidate-heading"
      >
        <header>
          <div>
            <span>Change identity</span><h2 id="candidate-heading">
              ChangeCandidate
            </h2>
          </div><UBadge
            color="neutral"
            variant="soft"
            :label="String(candidateRows.length)"
          />
        </header>
        <UTable
          :data="candidateRows"
          :columns="candidateColumns"
          empty="无持久化 ChangeCandidate。"
          sticky="header"
        />
      </section>
      <section
        class="identity-section"
        aria-labelledby="baseline-heading"
      >
        <header>
          <div>
            <span>Verified deployment identity</span><h2 id="baseline-heading">
              DeploymentBaseline
            </h2>
          </div><UBadge
            color="neutral"
            variant="soft"
            :label="String(baselineRows.length)"
          />
        </header>
        <UTable
          :data="baselineRows"
          :columns="baselineColumns"
          empty="无 verified DeploymentBaseline。"
          sticky="header"
        />
      </section>
      <section
        class="identity-section"
        aria-labelledby="delivery-heading"
      >
        <header>
          <div>
            <span>PR / CI / Argo / rollout</span><h2 id="delivery-heading">
              Delivery projection
            </h2>
          </div><UBadge
            color="neutral"
            variant="soft"
            :label="String(deliveryRows.length)"
          />
        </header>
        <UTable
          :data="deliveryRows"
          :columns="deliveryColumns"
          empty="GitHub/Argo delivery branch NOT RUN。"
          sticky="header"
        />
      </section>
    </main>

    <WorkspaceInspector
      :open="Boolean(inspectorID)"
      title="DevOps Inspector"
      description="ExactIdentity、Authority 与当前执行链的只读压缩摘要。"
      :target-state="inspectorTargetState"
      target-description="selected Query 未匹配当前 DevOps projection；不会静默选择第一行。"
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
          <ResultBadge :result="inspectorSubject.status" />
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
          <div><dt>Execution</dt><dd>{{ inspectorExecution?.status || "NOT RUN" }}</dd></div>
          <div><dt>Delivery</dt><dd>{{ inspectorDelivery?.status || "NOT RUN" }}</dd></div>
          <div><dt>Verification</dt><dd>{{ inspectorExecution?.verification?.status || "NOT RUN" }}</dd></div>
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
            label="Approval"
            :to="incidentStageHref(inspectorOwnership.incidentID, 'approval')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-git-pull-request-arrow"
            label="Delivery"
            :to="incidentStageHref(inspectorOwnership.incidentID, 'delivery')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-shield-check"
            label="Verification"
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
      :confirm-label="confirmationMode === 'authorize' ? '授权 exact hash' : confirmationMode === 'execute' ? '排队执行' : '创建 exact Plan'"
      :reason-required="confirmationMode === 'authorize'"
      :pending="confirmationPending"
      :severity="confirmationMode === 'execute' ? 'error' : 'warning'"
      @update:open="(open) => { if (!open) confirmationMode = '' }"
      @confirm="confirmCommand"
    />
  </section>
</template>

<style scoped>
.devops-workspace { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-4); }
.header-facts { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-4); color: var(--co-text-secondary); font-size: 11px; }
.header-facts span { display: inline-flex; align-items: baseline; gap: var(--co-space-1); }
.header-facts strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 16px; font-variant-numeric: tabular-nums; }
.provider-strip, .queue-section, .freeze-section, .scenario-strip, .detail-section, .identity-section { min-width: 0; border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
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
.error-identities { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.toolbar-summary { display: grid; min-width: 0; gap: 2px; }
.toolbar-summary span { color: var(--co-text-muted); font-size: 10px; }
.workspace-loading { display: grid; gap: 1px; padding: var(--co-space-3); border-block: 1px solid var(--co-border-default); }
.loading-row { height: var(--co-table-row-height); }
.operations-index, .operation-detail, .identity-view, .devops-inspector { display: grid; min-width: 0; gap: var(--co-space-4); }
.scenario-strip { display: grid; grid-template-columns: minmax(240px, .8fr) minmax(420px, 1.5fr) auto; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3); }
.scenario-strip > div { min-width: 0; }
.scenario-strip > div > span, .detail-heading > div > span, .detail-section > header span, .identity-section > header span, .devops-inspector header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scenario-strip h2 { margin: 2px 0 0; font-size: 15px; }
.scenario-strip p { margin: 2px 0 0; color: var(--co-text-secondary); font-size: 11px; }
.scenario-strip dl { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; }
.scenario-strip dl div { min-width: 0; padding-inline: var(--co-space-3); border-left: 1px solid var(--co-border-default); }
.scenario-strip dt, .fact-grid dt, .inspector-facts dt, .verification-matrix dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scenario-strip dd, .fact-grid dd, .inspector-facts dd, .verification-matrix dd { min-width: 0; margin: 2px 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.queue-section > header, .freeze-section > header, .identity-section > header { padding: var(--co-space-3); }
.devops-table-stack { display: grid; min-width: 0; justify-items: start; gap: 3px; }
.devops-table-stack strong { color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.devops-table-stack span, .muted-copy { color: var(--co-text-muted); font-size: 11px; }
.devops-table-stack code, .dense-hash, .identity-hash { min-width: 0; max-width: 100%; overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.freeze-section ul { margin: 0; padding: 0; list-style: none; }
.freeze-section li { display: grid; min-width: 0; min-height: 52px; grid-template-columns: minmax(240px, .7fr) auto minmax(260px, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.freeze-section li > div { display: grid; min-width: 0; }
.freeze-section li p { color: var(--co-text-secondary); overflow-wrap: anywhere; }
.empty-copy { margin: 0; padding: var(--co-space-4); color: var(--co-text-muted); }
.detail-heading { padding: var(--co-space-4); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
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
.devops-inspector > header > div { display: grid; min-width: 0; gap: 2px; }
.devops-inspector header code { min-width: 0; color: var(--co-text-muted); overflow-wrap: anywhere; }
.inspector-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 0; }
.inspector-stage-links { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }

@media (max-width: 1180px) {
  .scenario-strip { grid-template-columns: minmax(0, 1fr) auto; }
  .scenario-strip dl { grid-column: 1 / -1; grid-row: 2; }
  .provider-strip ul { grid-template-columns: minmax(0, 1fr); }
  .provider-strip li + li { border-left: 0; }
  .delivery-rail { grid-template-columns: minmax(0, 1fr); }
  .delivery-rail li + li { border-left: 0; }
}

@media (max-width: 900px) {
  .scenario-strip { grid-template-columns: minmax(0, 1fr); }
  .scenario-strip dl { grid-column: auto; grid-row: auto; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .scenario-strip dl div { border-left: 0; border-bottom: 1px solid var(--co-border-default); padding: var(--co-space-2) 0; }
  .freeze-section li { grid-template-columns: minmax(0, 1fr) auto; }
  .freeze-section li p, .freeze-section li time { grid-column: 1 / -1; }
  .hash-grid, .delivery-identities, .fact-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
