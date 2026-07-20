<script setup lang="ts">
import { ref } from "vue";

import { formatDurationMS, formatJSON, safeExternalURL } from "../../models/workbench";
import type { LoadState, VerificationRunView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import JSONSnapshot from "./JSONSnapshot.vue";

withDefaults(defineProps<{
  state: LoadState;
  error: string;
  runs: VerificationRunView[];
  nextCursor?: string;
}>(), { nextCursor: "" });

defineEmits<{ loadMore: [] }>();

const expandedChecks = ref(new Set<string>());
const sampleLimits = ref<Record<string, number>>({});

function toggleCheck(checkID: string, event: Event) {
  const next = new Set(expandedChecks.value);
  if ((event.currentTarget as HTMLDetailsElement).open) next.add(checkID);
  else next.delete(checkID);
  expandedChecks.value = next;
}

function visibleSamples(checkID: string): number {
  return sampleLimits.value[checkID] ?? 50;
}

function showMoreSamples(checkID: string, total: number) {
  sampleLimits.value = {
    ...sampleLimits.value,
    [checkID]: Math.min(visibleSamples(checkID) + 50, total),
  };
}
</script>

<template>
  <IncidentSectionShell
    id="verifications"
    title="Recovery verification"
    :state="state"
    :error="error"
    empty-text="No deterministic VerificationRun exists for this cycle."
  >
    <div class="run-stack">
      <article
        v-for="run in runs"
        :key="run.id"
        class="run"
      >
        <header class="run__header">
          <div>
            <span class="eyebrow">VerificationRun {{ run.id }}</span>
            <h3>{{ run.profile.id }}</h3>
            <p>{{ run.trigger_type.replace(/_/g, " ") }} · attempt {{ run.attempt }}</p>
          </div>
          <IncidentStatusBadge :status="run.status" />
        </header>

        <div
          v-if="run.status === 'inconclusive' || run.status === 'timed_out'"
          class="truth-banner"
          role="status"
        >
          <strong>{{ run.status.replace(/_/g, " ") }}</strong>
          <span>{{ run.failure_reason || run.result_summary || "Recovery was not proved." }}</span>
        </div>

        <dl class="fact-grid">
          <div><dt>Profile</dt><dd>{{ run.profile.id }} v{{ run.profile.version }} · contract {{ run.profile.contract_version }}</dd></div>
          <div><dt>Common window</dt><dd>{{ formatDurationMS(run.common_window.stability_window_ms) }}</dd></div>
          <div><dt>Started</dt><dd>{{ formatIncidentTime(run.started_at) }}</dd></div>
          <div><dt>Deadline</dt><dd>{{ formatIncidentTime(run.deadline_at) }}</dd></div>
          <div><dt>Success since</dt><dd>{{ formatIncidentTime(run.common_window.success_since) }}</dd></div>
          <div><dt>Completed</dt><dd>{{ formatIncidentTime(run.completed_at || run.common_window.completed_at) }}</dd></div>
        </dl>

        <div
          v-if="run.result_summary"
          class="result-summary"
        >
          {{ run.result_summary }}
        </div>

        <section
          class="identity-block"
          :aria-labelledby="`verification-identities-${run.id}`"
        >
          <h4 :id="`verification-identities-${run.id}`">
            Verified identities
          </h4>
          <HashValue
            label="Profile hash"
            :value="run.profile.hash"
          />
          <HashValue
            label="Target revision"
            :value="run.revisions.target_revision"
          />
          <HashValue
            label="Source revision"
            :value="run.revisions.source_revision"
          />
          <HashValue
            label="Image digest"
            :value="run.revisions.image_digest"
          />
          <HashValue
            label="GitOps revision"
            :value="run.revisions.gitops_revision"
          />
        </section>

        <section
          class="checks"
          :aria-labelledby="`checks-${run.id}`"
        >
          <div class="checks__heading">
            <h4 :id="`checks-${run.id}`">
              Checks and samples
            </h4>
            <span>{{ run.checks.length }} persisted checks</span>
          </div>
          <details
            v-for="check in run.checks"
            :key="check.id"
            class="check"
            @toggle="toggleCheck(check.id, $event)"
          >
            <summary>
              <span class="check__identity">
                <strong>{{ check.template_id }}</strong>
                <small>{{ check.type }} · {{ check.required ? "Required" : "Optional" }} · {{ check.samples.length }} samples</small>
              </span>
              <IncidentStatusBadge :status="check.status" />
            </summary>
            <div
              v-if="expandedChecks.has(check.id)"
              class="check__body"
            >
              <dl class="fact-grid compact">
                <div><dt>Subject</dt><dd>{{ check.subject.workload_kind || "subject" }}/{{ check.subject.workload_name || check.subject.service || "Not projected" }}</dd></div>
                <div><dt>Source identity</dt><dd>{{ check.source_identity }}</dd></div>
                <div><dt>Comparison</dt><dd>{{ check.comparison || "server-defined" }} {{ check.threshold ?? "" }}</dd></div>
                <div><dt>Samples</dt><dd>{{ check.samples.length }} / minimum {{ check.min_samples }} {{ check.sample_unit }}</dd></div>
                <div><dt>Poll / timeout</dt><dd>{{ formatDurationMS(check.poll_interval_ms) }} / {{ formatDurationMS(check.timeout_ms) }}</dd></div>
                <div><dt>Stability</dt><dd>{{ formatDurationMS(check.stability_window_ms) }} · {{ check.failure_mode }}</dd></div>
              </dl>

              <a
                v-if="safeExternalURL(check.source_reference)"
                class="source-link"
                :href="safeExternalURL(check.source_reference)"
                target="_blank"
                rel="noopener noreferrer"
              >Open persisted source reference</a>
              <code
                v-else-if="check.source_reference"
                class="source-reference"
              >{{ check.source_reference }}</code>

              <div
                v-if="check.failure_reason"
                class="check-failure"
                role="status"
              >
                {{ check.failure_reason }}
              </div>

              <div class="snapshot-grid">
                <JSONSnapshot
                  title="Expected fact"
                  :value="check.expected"
                />
                <JSONSnapshot
                  title="Latest observed fact"
                  :value="check.observed"
                />
                <JSONSnapshot
                  title="Bounded subject"
                  :value="check.subject"
                />
              </div>

              <div
                v-if="check.samples.length"
                class="sample-table-wrap"
                tabindex="0"
                aria-label="Persisted verification samples; horizontally scrollable on small screens"
              >
                <table>
                  <thead>
                    <tr><th>Seq</th><th>Status</th><th>Sampled</th><th>Window</th><th>Observed</th><th>Hash / reason</th></tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="sample in check.samples.slice(0, visibleSamples(check.id))"
                      :key="sample.id"
                    >
                      <td>{{ sample.sequence }}</td>
                      <td><IncidentStatusBadge :status="sample.status" /></td>
                      <td>{{ formatIncidentTime(sample.sampled_at) }}</td>
                      <td>{{ formatIncidentTime(sample.window_start_at) }} → {{ formatIncidentTime(sample.window_end_at) }}</td>
                      <td><pre><code>{{ formatJSON(sample.observed) }}</code></pre></td>
                      <td><code>{{ sample.content_hash }}</code><span v-if="sample.reason_code">{{ sample.reason_code }}</span></td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div
                v-if="check.samples.length > visibleSamples(check.id)"
                class="sample-actions"
              >
                <span>{{ visibleSamples(check.id) }} of {{ check.samples.length }} samples shown</span>
                <el-button @click="showMoreSamples(check.id, check.samples.length)">
                  Show 50 More Samples
                </el-button>
              </div>
              <p
                v-else
                class="no-samples"
              >
                No persisted samples. This is not a passing check.
              </p>
            </div>
          </details>
        </section>
      </article>
    </div>
    <el-button
      v-if="nextCursor"
      class="load-more"
      @click="$emit('loadMore')"
    >
      Load next persisted page
    </el-button>
  </IncidentSectionShell>
</template>

<style scoped>
.run-stack { display: grid; gap: 20px; }
.run { display: grid; gap: 18px; min-width: 0; padding: 18px; border: 1px solid var(--cloudops-border-color); border-radius: 9px; background: var(--el-fill-color-lighter); }
.run__header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.run__header h3 { margin: 4px 0; }
.run__header p { margin: 0; color: var(--el-text-color-secondary); text-transform: capitalize; }
.eyebrow, dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .04em; }
.truth-banner { display: grid; gap: 3px; padding: 12px; border-left: 4px solid var(--el-color-warning); background: var(--el-color-warning-light-9); text-transform: capitalize; }
.fact-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin: 0; }
.fact-grid div { min-width: 0; padding-bottom: 9px; border-bottom: 1px solid var(--cloudops-border-color); }
.fact-grid.compact { grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); }
dd { margin: 4px 0 0; overflow-wrap: anywhere; }
.result-summary { padding: 12px; border-left: 3px solid var(--el-color-primary); background: var(--cloudops-bg-card); }
.identity-block h4, .checks h4 { margin: 0 0 8px; }
.checks__heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.checks__heading span { color: var(--el-text-color-secondary); font-size: 12px; }
.check { border-top: 1px solid var(--cloudops-border-color); }
.check > summary { display: flex; justify-content: space-between; align-items: center; gap: 16px; min-height: 56px; padding: 10px 0; cursor: pointer; }
.check > summary:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.check__identity { display: grid; min-width: 0; }
.check__identity small { color: var(--el-text-color-secondary); }
.check__body { display: grid; gap: 14px; padding: 6px 0 18px 22px; }
.source-link { width: fit-content; color: var(--el-color-primary); text-decoration: underline; text-underline-offset: 3px; }
.source-reference { overflow-wrap: anywhere; }
.check-failure { padding: 10px; color: var(--el-color-danger-dark-2); background: var(--el-color-danger-light-9); }
.snapshot-grid { display: grid; }
.sample-table-wrap { max-width: 100%; overflow: auto; border: 1px solid var(--cloudops-border-color); border-radius: 7px; background: var(--cloudops-bg-card); }
.sample-table-wrap:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
table { width: 100%; min-width: 920px; border-collapse: collapse; font-size: 12px; }
th, td { padding: 10px; border-bottom: 1px solid var(--cloudops-border-color); text-align: left; vertical-align: top; }
th { position: sticky; top: 0; z-index: 1; background: var(--el-fill-color-light); }
tbody { font-variant-numeric: tabular-nums; }
td pre { max-width: 360px; max-height: 160px; margin: 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
td:last-child { max-width: 300px; }
td:last-child code, td:last-child span { display: block; overflow-wrap: anywhere; }
.no-samples { margin: 0; color: var(--el-text-color-secondary); }
.sample-actions { display: flex; justify-content: space-between; align-items: center; gap: 12px; color: var(--el-text-color-secondary); font-size: 12px; }
.load-more { margin-top: 16px; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 11px; }
@media (max-width: 640px) { .run { padding: 14px; } .run__header, .checks__heading { align-items: flex-start; flex-direction: column; } .check__body { padding-left: 0; } }
</style>
