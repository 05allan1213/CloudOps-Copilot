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
  if (props.report.diagnosis === null) missing.push("此报告没有 Diagnosis。");
  if (props.report.trigger_type !== "operational_recovery") {
    if (props.report.remediation_plan === null) missing.push("没有 remediation Plan；这可能是无变更恢复。");
    if (props.report.remediation_decision === null) missing.push("没有 remediation Decision。");
    if (props.report.delivery === null) missing.push("没有 Delivery；此报告不代表发生过外部写入。");
  }
  return missing;
});

function reportCodeLabel(value: string): string {
  return ({
    no_change_signal: "无变更信号",
    post_delivery: "Delivery 后验证",
    operational_recovery: "运行恢复验证",
    verification_passed: "Verification 已通过",
    recovered: "已证明恢复",
  } as Record<string, string>)[value] ?? value.replace(/_/g, " ");
}
</script>

<template>
  <IncidentSectionShell
    id="resolution-report"
    title="ResolutionReport"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :retryable="true"
    empty-text="尚无不可变 ResolutionReport，因此不会把恢复显示为已证明。"
    projection-note="仅当最新持久化 VerificationRun 通过后才公开此报告。"
    @retry="emit('retry')"
  >
    <div
      v-if="report && !eligible"
      class="report-withheld"
      role="alert"
    >
      <strong>ResolutionReport 已被界面隐藏。</strong>
      <span>当前 Verification 投影未通过。请刷新服务端投影；浏览器不会把此不一致显示为成功。</span>
    </div>

    <article
      v-else-if="report"
      class="report"
    >
      <header class="report-header">
        <div>
          <span>ResolutionReport {{ report.id }}</span>
          <h3>{{ report.summary }}</h3>
          <p>{{ reportCodeLabel(report.resolution_reason) }} · {{ reportCodeLabel(report.trigger_type) }}</p>
        </div>
        <ResultBadge :result="report.status" />
      </header>

      <div
        v-if="report.trigger_type === 'no_change_signal' || report.trigger_type === 'operational_recovery'"
        class="no-change-banner"
        role="status"
      >
        <strong>{{ report.trigger_type === "operational_recovery" ? "运行恢复" : "无变更恢复" }}</strong>
        <span>除非对应持久化区块存在，否则此报告不会声称存在 Plan、批准、PR、Argo 同步或 rollout。</span>
      </div>

      <section
        class="impact"
        aria-labelledby="resolution-impact"
      >
        <h4 id="resolution-impact">
          实测结果
        </h4>
        <p>{{ report.impact_summary }}</p>
      </section>

      <dl class="report-facts">
        <div><dt>Service / Workload</dt><dd>{{ report.service }} / {{ report.workload }}</dd></div>
        <div><dt>环境</dt><dd>{{ report.environment }}</dd></div>
        <div><dt>Cycle</dt><dd>{{ report.cycle }}</dd></div>
        <div><dt>Cycle 开始</dt><dd>{{ formatIncidentTime(report.cycle_started_at) }}</dd></div>
        <div><dt>恢复时间</dt><dd>{{ formatIncidentTime(report.resolved_at) }}</dd></div>
        <div><dt>实测时长</dt><dd>{{ formatDurationMS(report.measured_duration_ms) }}</dd></div>
        <div><dt>生成时间</dt><dd>{{ formatIncidentTime(report.generated_at) }}</dd></div>
        <div><dt>稳定窗口</dt><dd>{{ formatIncidentTime(report.stability.common_window_started_at) }} → {{ formatIncidentTime(report.stability.common_window_completed_at) }}</dd></div>
      </dl>

      <section
        v-if="report.recovery_provenance"
        class="identity-section"
        aria-labelledby="resolution-provenance"
      >
        <h4 id="resolution-provenance">恢复 Provenance</h4>
        <div class="hash-grid">
          <HashValue label="Configuration Revision" :value="report.recovery_provenance.configuration_revision_id" />
          <HashValue label="Operational Scope" :value="report.recovery_provenance.operational_scope_id" />
          <HashValue label="Investigation" :value="report.recovery_provenance.investigation_id" />
          <HashValue label="Owner Decision" :value="report.recovery_provenance.decision_id" />
        </div>
      </section>

      <section
        class="identity-section"
        aria-labelledby="resolution-identities"
      >
        <h4 id="resolution-identities">
          不可变恢复身份
        </h4>
        <div class="hash-grid">
          <HashValue
            label="报告 Hash"
            :value="report.hash"
          />
          <HashValue
            label="Verification Profile"
            :value="report.verification_profile.hash"
          />
          <HashValue
            label="异常 GitOps Revision"
            :value="report.revisions.bad_gitops_revision"
          />
          <HashValue
            label="修复 GitOps Revision"
            :value="report.revisions.fix_gitops_revision"
          />
          <HashValue
            label="源码 Revision"
            :value="report.revisions.source_revision"
          />
          <HashValue
            label="镜像 Digest"
            :value="report.revisions.image_digest"
          />
          <HashValue
            label="已部署 GitOps Revision"
            :value="report.revisions.gitops_revision"
          />
        </div>
      </section>

      <section
        class="limits"
        aria-labelledby="resolution-limits"
      >
        <h4 id="resolution-limits">
          持久化边界与后续事项
        </h4>
        <ul>
          <li
            v-for="item in missingSections"
            :key="item"
          >
            {{ item }}
          </li>
          <li v-if="report.migrated_legacy_context">
            此报告包含迁移的 legacy context；provenance 保持显式。
          </li>
          <li>当前 ResolutionReport contract 不投影后续 action。</li>
        </ul>
      </section>

      <details class="report-package">
        <summary>查看可审计的持久化区块</summary>
        <div>
          <JSONSnapshot
            title="触发 Signal"
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
            title="Agent 使用情况"
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
