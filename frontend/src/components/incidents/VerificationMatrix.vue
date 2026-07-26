<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import {
  latestVerificationRun,
  stabilityProgress,
  verificationStateLabel,
} from "../../models/recovery";
import { formatDurationMS, formatJSON, safeExternalURL } from "../../models/workbench";
import type { LoadState, VerificationCheckView, VerificationRunView } from "../../types/incidents";
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
  runs: VerificationRunView[];
  nextCursor?: string;
  refreshing?: boolean;
  loadingMore?: boolean;
}>(), {
  error: null,
  nextCursor: "",
  refreshing: false,
  loadingMore: false,
});

const emit = defineEmits<{
  loadMore: [];
  retry: [];
}>();

const selectedRunID = ref("");
const sampleLimits = ref<Record<string, number>>({});
const now = ref(Date.now());
let clockTimer: number | null = null;

const orderedRuns = computed(() => [...props.runs].sort((left, right) => {
  if (right.attempt !== left.attempt) return right.attempt - left.attempt;
  return Date.parse(right.updated_at || right.created_at) - Date.parse(left.updated_at || left.created_at);
}));
const selectedRun = computed(() => orderedRuns.value.find((run) => run.id === selectedRunID.value) ?? latestVerificationRun(orderedRuns.value));
const progress = computed(() => selectedRun.value ? stabilityProgress(selectedRun.value, now.value) : null);

watch(orderedRuns, (runs) => {
  if (!runs.some((run) => run.id === selectedRunID.value)) selectedRunID.value = runs[0]?.id ?? "";
}, { immediate: true });

onMounted(() => {
  clockTimer = window.setInterval(() => { now.value = Date.now(); }, 5000);
});

onBeforeUnmount(() => {
  if (clockTimer !== null) window.clearInterval(clockTimer);
});

function visibleSamples(checkID: string): number {
  return sampleLimits.value[checkID] ?? 25;
}

function showMoreSamples(checkID: string, total: number) {
  sampleLimits.value = {
    ...sampleLimits.value,
    [checkID]: Math.min(visibleSamples(checkID) + 25, total),
  };
}

function predicateLabel(check: VerificationCheckView): string {
  const comparison = check.comparison || "server-defined";
  return check.threshold === undefined ? comparison : `${comparison} ${check.threshold}`;
}

function checkSourceURL(check: VerificationCheckView): string {
  return safeExternalURL(check.source_reference);
}

function statusExplanation(run: VerificationRunView): string {
  if (run.result_summary) return run.result_summary;
  if (run.failure_reason) return run.failure_reason;
  const explanations: Record<string, string> = {
    pending: "The deterministic run is queued; recovery has not been proved.",
    running: "Required checks are still being sampled; recovery has not been proved.",
    passed: "The server persisted a passing result for the complete required profile.",
    failed: "At least one required check failed; recovery was not proved.",
    timed_out: "The required stable window was not completed before the persisted deadline.",
    inconclusive: "The available observations were insufficient to prove recovery or failure.",
    cancelled: "The run ended without a recovery verdict.",
  };
  return explanations[run.status] || "The persisted run status is not recognized by this presentation.";
}
</script>

<template>
  <IncidentSectionShell
    id="verifications"
    title="Recovery Verification"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    :retryable="true"
    empty-text="Verification status: NOT RUN. No deterministic VerificationRun exists for this cycle."
    projection-note="Required and optional checks, samples, thresholds, and the common stable window are persisted server facts. Delivery health is not treated as recovery."
    @retry="emit('retry')"
  >
    <template v-if="selectedRun">
      <nav
        class="attempt-history"
        aria-label="Verification attempt history"
      >
        <span>Attempt History</span>
        <div>
          <button
            v-for="run in orderedRuns"
            :key="run.id"
            type="button"
            :class="{ 'is-active': run.id === selectedRun.id }"
            :aria-current="run.id === selectedRun.id ? 'true' : undefined"
            @click="selectedRunID = run.id"
          >
            <strong>Attempt {{ run.attempt }}</strong>
            <ResultBadge :result="run.status" />
            <small>{{ formatIncidentTime(run.completed_at || run.started_at || run.created_at) }}</small>
          </button>
        </div>
      </nav>

      <article class="verification-run">
        <header class="run-header">
          <div>
            <span>VerificationRun {{ selectedRun.id }}</span>
            <h3>{{ selectedRun.profile.id }}</h3>
            <p>{{ selectedRun.trigger_type.replace(/_/g, " ") }} · attempt {{ selectedRun.attempt }} · {{ verificationStateLabel(selectedRun.status) }}</p>
          </div>
          <ResultBadge :result="selectedRun.status" />
        </header>

        <div
          v-if="selectedRun.trigger_type === 'no_change_signal'"
          class="no-change-banner"
          role="status"
        >
          <strong>No-change verification path</strong>
          <span>This run does not imply a Plan, approval, pull request, or Delivery. Only its persisted checks can prove recovery.</span>
        </div>

        <div
          class="run-truth"
          :class="`run-truth--${selectedRun.status}`"
          role="status"
        >
          <strong>{{ verificationStateLabel(selectedRun.status) }}</strong>
          <span>{{ statusExplanation(selectedRun) }}</span>
        </div>

        <section
          v-if="progress"
          class="common-window"
          aria-labelledby="common-window-title"
        >
          <div>
            <span>Common Stable Window</span>
            <h4 id="common-window-title">
              {{ progress.label }}
            </h4>
            <p v-if="progress.source === 'not_projected'">
              No common success start is projected. This is not a passing window.
            </p>
            <p v-else-if="progress.source === 'persisted_success_since'">
              Elapsed presentation from persisted <code>success_since</code>; the run status remains authoritative.
            </p>
            <p v-else>
              The server persisted completion of the full common window.
            </p>
          </div>
          <progress
            :value="progress.elapsedMs"
            :max="progress.requiredMs || 1"
            :aria-label="`Common verification window ${progress.label}`"
          />
        </section>

        <dl class="run-facts">
          <div><dt>Profile</dt><dd>{{ selectedRun.profile.id }} v{{ selectedRun.profile.version }} · contract {{ selectedRun.profile.contract_version }}</dd></div>
          <div><dt>Started</dt><dd>{{ formatIncidentTime(selectedRun.started_at) }}</dd></div>
          <div><dt>Deadline</dt><dd>{{ formatIncidentTime(selectedRun.deadline_at) }}</dd></div>
          <div><dt>Completed</dt><dd>{{ formatIncidentTime(selectedRun.completed_at) }}</dd></div>
          <div><dt>Success Since</dt><dd>{{ formatIncidentTime(selectedRun.common_window.success_since) }}</dd></div>
          <div><dt>Provenance</dt><dd>{{ selectedRun.migrated_legacy_context ? "Migrated legacy context" : "Native projection" }}</dd></div>
        </dl>

        <section
          class="identity-section"
          :aria-labelledby="`verification-identity-${selectedRun.id}`"
        >
          <h4 :id="`verification-identity-${selectedRun.id}`">
            Exact Verified Identity
          </h4>
          <div class="hash-grid">
            <HashValue
              label="Profile Hash"
              :value="selectedRun.profile.hash"
            />
            <HashValue
              label="Target Revision"
              :value="selectedRun.revisions.target_revision"
            />
            <HashValue
              label="Source Revision"
              :value="selectedRun.revisions.source_revision"
            />
            <HashValue
              label="Image Digest"
              :value="selectedRun.revisions.image_digest"
            />
            <HashValue
              label="GitOps Revision"
              :value="selectedRun.revisions.gitops_revision"
            />
          </div>
        </section>

        <section
          class="check-matrix"
          :aria-labelledby="`verification-checks-${selectedRun.id}`"
        >
          <div class="matrix-heading">
            <div>
              <span>Verification Matrix</span>
              <h4 :id="`verification-checks-${selectedRun.id}`">
                {{ selectedRun.checks.length }} Persisted Check{{ selectedRun.checks.length === 1 ? "" : "s" }}
              </h4>
            </div>
            <small>{{ selectedRun.checks.filter((check) => check.required).length }} required · {{ selectedRun.checks.filter((check) => !check.required).length }} optional</small>
          </div>

          <div
            class="matrix-columns"
            aria-hidden="true"
          >
            <span>Requirement / Check</span>
            <span>Result</span>
            <span>Predicate</span>
            <span>Samples</span>
            <span>Stable Window</span>
          </div>

          <details
            v-for="check in selectedRun.checks"
            :key="check.id"
            class="check-row"
          >
            <summary>
              <span class="check-identity">
                <small>{{ check.required ? "Required" : "Optional" }}</small>
                <strong>{{ check.template_id }}</strong>
                <code translate="no">{{ check.type }}</code>
              </span>
              <span
                class="matrix-cell"
                data-label="Result"
              >
                <ResultBadge :result="check.status" />
              </span>
              <span
                class="matrix-cell"
                data-label="Predicate"
              >{{ predicateLabel(check) }}</span>
              <span
                class="matrix-cell"
                data-label="Samples"
              >{{ check.samples.length }} / min {{ check.min_samples }} {{ check.sample_unit }}</span>
              <span
                class="matrix-cell"
                data-label="Stable Window"
              >{{ formatDurationMS(check.stability_window_ms) }}</span>
            </summary>

            <div class="check-body">
              <dl class="check-facts">
                <div><dt>Check ID</dt><dd><code translate="no">{{ check.id }}</code></dd></div>
                <div><dt>Source Identity</dt><dd>{{ check.source_identity }}</dd></div>
                <div><dt>Profile / Template</dt><dd>{{ check.profile_id }} · {{ check.template_version }}</dd></div>
                <div><dt>Poll / Timeout</dt><dd>{{ formatDurationMS(check.poll_interval_ms) }} / {{ formatDurationMS(check.timeout_ms) }}</dd></div>
                <div><dt>Attempts</dt><dd>{{ check.attempt_count }}</dd></div>
                <div><dt>Consecutive Success</dt><dd>{{ formatIncidentTime(check.consecutive_success_since) }}</dd></div>
              </dl>

              <a
                v-if="checkSourceURL(check)"
                class="source-link"
                :href="checkSourceURL(check)"
                target="_blank"
                rel="noopener noreferrer"
              >Open persisted source reference in new tab</a>
              <code
                v-else-if="check.source_reference"
                class="source-reference"
                translate="no"
              >{{ check.source_reference }}</code>

              <div
                v-if="check.failure_reason"
                class="check-failure"
                role="status"
              >
                <strong>{{ verificationStateLabel(check.status) }}</strong>
                <span>{{ check.failure_reason }}</span>
              </div>

              <div class="snapshot-grid">
                <JSONSnapshot
                  title="Expected Fact"
                  :value="check.expected"
                />
                <JSONSnapshot
                  title="Latest Observed Fact"
                  :value="check.observed"
                />
                <JSONSnapshot
                  title="Bounded Subject"
                  :value="check.subject"
                />
              </div>

              <section
                class="sample-history"
                :aria-labelledby="`samples-${check.id}`"
              >
                <div class="sample-heading">
                  <h5 :id="`samples-${check.id}`">
                    Persisted Sample History
                  </h5>
                  <span>{{ Math.min(visibleSamples(check.id), check.samples.length) }} / {{ check.samples.length }} shown</span>
                </div>
                <p
                  v-if="check.samples.length === 0"
                  class="no-samples"
                  role="status"
                >
                  No persisted samples. This check is not presented as passing.
                </p>
                <ol v-else>
                  <li
                    v-for="sample in check.samples.slice(0, visibleSamples(check.id))"
                    :key="sample.id"
                  >
                    <div>
                      <strong>Sample {{ sample.sequence }}</strong>
                      <ResultBadge :result="sample.status" />
                    </div>
                    <dl>
                      <div><dt>Sampled</dt><dd>{{ formatIncidentTime(sample.sampled_at) }}</dd></div>
                      <div><dt>Window</dt><dd>{{ formatIncidentTime(sample.window_start_at) }} → {{ formatIncidentTime(sample.window_end_at) }}</dd></div>
                      <div><dt>Hash</dt><dd><code translate="no">{{ sample.content_hash }}</code></dd></div>
                      <div><dt>Reason</dt><dd>{{ sample.reason_code || "No reason code" }}</dd></div>
                    </dl>
                    <pre><code>{{ formatJSON(sample.observed) }}</code></pre>
                  </li>
                </ol>
                <button
                  v-if="check.samples.length > visibleSamples(check.id)"
                  type="button"
                  class="show-more"
                  @click="showMoreSamples(check.id, check.samples.length)"
                >
                  Show 25 More Samples
                </button>
              </section>
            </div>
          </details>
        </section>
      </article>
    </template>

    <button
      v-if="nextCursor"
      type="button"
      class="load-more"
      :disabled="loadingMore"
      @click="emit('loadMore')"
    >
      {{ loadingMore ? "Loading More Attempts…" : "Load More Verification Attempts" }}
    </button>
  </IncidentSectionShell>
</template>

<style scoped>
.attempt-history,
.verification-run,
.identity-section,
.check-matrix,
.check-body,
.sample-history {
  display: grid;
  min-width: 0;
}

.attempt-history { gap: var(--co-space-2); }
.attempt-history > span,
.run-header span,
.common-window span,
.matrix-heading span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.attempt-history > div { display: flex; min-width: 0; gap: var(--co-space-2); overflow-x: auto; padding-bottom: var(--co-space-1); }
.attempt-history button { display: grid; min-width: 180px; min-height: 68px; justify-items: start; gap: var(--co-space-1); padding: var(--co-space-2) var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); cursor: pointer; }
.attempt-history button.is-active { border-color: var(--co-action-primary); background: var(--co-bg-active); box-shadow: inset 0 -2px 0 var(--co-action-primary); }
.attempt-history button small { color: var(--co-text-muted); }

.verification-run { gap: var(--co-space-5); padding: var(--co-space-5); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.run-header,
.matrix-heading,
.sample-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.run-header > div,
.matrix-heading > div { min-width: 0; }
.run-header h3,
.matrix-heading h4 { margin: 3px 0 0; color: var(--co-text-primary); overflow-wrap: anywhere; }
.run-header h3 { font-size: 18px; }
.run-header p { margin: 3px 0 0; color: var(--co-text-secondary); text-transform: capitalize; }

.no-change-banner,
.run-truth,
.check-failure { display: grid; gap: 2px; padding: var(--co-space-3) var(--co-space-4); border-left: 3px solid var(--co-status-neutral-border); color: var(--co-text-secondary); background: var(--co-bg-subtle); }
.run-truth--failed { border-left-color: var(--co-status-critical-fg); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.run-truth--timed_out,
.run-truth--pending { border-left-color: var(--co-status-warning-fg); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.run-truth--inconclusive { border-left-color: var(--co-status-inconclusive-fg); color: var(--co-status-inconclusive-fg); background: var(--co-status-inconclusive-bg); }
.run-truth--running { border-left-color: var(--co-status-info-fg); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.run-truth--passed { border-left-color: var(--co-status-success-fg); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }

.common-window { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) minmax(220px, .42fr); align-items: center; gap: var(--co-space-5); padding: var(--co-space-4); border: 1px solid var(--co-border-default); background: var(--co-bg-subtle); }
.common-window h4 { margin: 3px 0 0; font-size: 18px; }
.common-window p { margin: var(--co-space-1) 0 0; color: var(--co-text-muted); font-size: 12px; }
.common-window progress { width: 100%; height: 12px; accent-color: var(--co-action-primary); }

.run-facts,
.check-facts,
.sample-history dl { display: grid; min-width: 0; margin: 0; }
.run-facts { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); }
.run-facts div,
.check-facts div,
.sample-history dl div { min-width: 0; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.run-facts dt,
.check-facts dt,
.sample-history dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.run-facts dd,
.check-facts dd,
.sample-history dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

.identity-section,
.check-matrix { gap: var(--co-space-3); }
.identity-section > h4 { margin: 0; font-size: 15px; }
.hash-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--co-space-5); }
.matrix-heading { align-items: center; }
.matrix-heading h4 { font-size: 15px; }
.matrix-heading small { color: var(--co-text-muted); }
.matrix-columns,
.check-row > summary { display: grid; min-width: 0; grid-template-columns: minmax(210px, 1.4fr) minmax(120px, .7fr) minmax(120px, .7fr) minmax(150px, .8fr) minmax(120px, .7fr); gap: var(--co-space-3); align-items: center; }
.matrix-columns { padding: var(--co-space-2) var(--co-space-3); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); background: var(--co-bg-subtle); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.check-row { border-bottom: 1px solid var(--co-border-default); }
.check-row > summary { min-height: 72px; padding: var(--co-space-3); cursor: pointer; }
.check-row[open] > summary { background: var(--co-bg-active); }
.check-identity { display: grid; min-width: 0; }
.check-identity small { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.check-identity strong,
.check-identity code { overflow-wrap: anywhere; }
.check-identity code { color: var(--co-text-muted); font-size: 11px; }
.matrix-cell { min-width: 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

.check-body { gap: var(--co-space-4); padding: var(--co-space-4); background: var(--co-bg-subtle); }
.check-facts { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); }
.source-link { width: fit-content; min-height: 44px; display: inline-flex; align-items: center; color: var(--co-action-primary); font-weight: 700; text-decoration: underline; text-underline-offset: 3px; }
.source-reference { overflow-wrap: anywhere; }
.check-failure { border-left-color: var(--co-status-critical-fg); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.snapshot-grid { display: grid; min-width: 0; }

.sample-history { gap: var(--co-space-3); }
.sample-heading { align-items: center; }
.sample-heading h5 { margin: 0; font-size: 14px; }
.sample-heading span { color: var(--co-text-muted); font-size: 12px; }
.sample-history ol { display: grid; gap: var(--co-space-3); margin: 0; padding: 0; list-style: none; }
.sample-history li { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-3); border: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.sample-history li > div { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.sample-history dl { grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--co-space-3); }
.sample-history pre { max-height: 180px; margin: 0; padding: var(--co-space-3); overflow: auto; border: 1px solid var(--co-border-default); background: var(--co-bg-canvas); white-space: pre-wrap; overflow-wrap: anywhere; }
.no-samples { margin: 0; color: var(--co-status-warning-fg); }
.show-more,
.load-more { width: fit-content; min-height: 44px; padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); font-weight: 700; cursor: pointer; }
.show-more:hover,
.load-more:hover { border-color: var(--co-action-primary); background: var(--co-bg-hover); }
.load-more:disabled { cursor: wait; opacity: .65; }

@media (max-width: 1000px) {
  .matrix-columns { display: none; }
  .check-row > summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .check-identity { grid-column: 1 / -1; }
  .matrix-cell::before { display: block; margin-bottom: 2px; color: var(--co-text-muted); content: attr(data-label); font-size: 10px; font-weight: 700; text-transform: uppercase; }
}

@media (max-width: 760px) {
  .verification-run { padding: var(--co-space-4); }
  .run-header,
  .matrix-heading,
  .sample-heading { align-items: flex-start; flex-direction: column; }
  .common-window,
  .run-facts,
  .hash-grid,
  .check-facts,
  .sample-history dl,
  .check-row > summary { grid-template-columns: minmax(0, 1fr); }
  .check-identity { grid-column: auto; }
  .show-more,
  .load-more { width: 100%; }
}
</style>
