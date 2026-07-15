<script setup lang="ts">
import { factTone } from "../../models/incidents";
import type { ClassifiedFactDTO, LoadState, PostmortemDTO } from "../../types/incidents";
import { formatDuration, formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; postmortem: PostmortemDTO | null }>();

function factClass(item: ClassifiedFactDTO): string {
  return factTone(item.classification);
}
</script>

<template>
  <IncidentSectionShell
    id="postmortem"
    title="Postmortem"
    :state="state"
    :error="error"
    empty-text="Not generated"
  >
    <template v-if="postmortem">
      <div class="postmortem-heading">
        <div><h3>{{ postmortem.title }}</h3><p>{{ postmortem.impact_summary || "Unknown impact" }}</p></div><small>Version {{ postmortem.generation_version }} · {{ formatIncidentTime(postmortem.generated_at) }}</small>
      </div>
      <el-descriptions
        :column="2"
        border
      >
        <el-descriptions-item label="Duration">
          {{ formatDuration(postmortem.duration_seconds) }}
        </el-descriptions-item>
        <el-descriptions-item label="Service / workload">
          {{ postmortem.service || "Unknown" }} / {{ postmortem.workload || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item label="Environment">
          {{ postmortem.environment || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item label="Exact revision">
          <code>{{ postmortem.delivery_revision || "Unknown" }}</code>
        </el-descriptions-item>
        <el-descriptions-item
          label="Verification"
          :span="2"
        >
          {{ postmortem.verification_summary || "Unknown" }}
        </el-descriptions-item>
      </el-descriptions>
      <div
        class="fact-grid"
        aria-label="Postmortem classified statements"
      >
        <article
          v-for="item in [postmortem.triggering_signal, postmortem.change_correlation, postmortem.root_cause, postmortem.remediation_summary, postmortem.approval_summary]"
          :key="`${item.classification}-${item.summary}`"
          :class="factClass(item)"
        >
          <strong>{{ factClass(item) }}</strong><p>{{ item.summary || "Unknown" }}</p><small>Evidence: {{ item.evidence_ids?.join(", ") || "Unknown" }}</small>
        </article>
      </div>
      <div class="postmortem-columns">
        <div>
          <h3>Bounded timeline</h3><ol>
            <li
              v-for="item in postmortem.timeline"
              :key="`${item.occurred_at}-${item.event_type}`"
            >
              <strong>{{ item.event_type }}</strong> — {{ item.summary || "Unknown" }} <small>{{ formatIncidentTime(item.occurred_at) }}</small>
            </li>
          </ol>
        </div>
        <div>
          <h3>Follow-ups</h3><ul>
            <li
              v-for="item in postmortem.follow_up_actions"
              :key="item"
            >
              {{ item }}
            </li><li v-if="postmortem.follow_up_actions.length === 0">
              None persisted
            </li>
          </ul>
        </div>
      </div>
    </template>
  </IncidentSectionShell>
</template>

<style scoped>
.postmortem-heading { display: flex; justify-content: space-between; gap: 16px; align-items: start; margin-bottom: 16px; }
.postmortem-heading h3, .postmortem-heading p { margin: 0 0 6px; }
.postmortem-heading small, article small, li small { color: var(--el-text-color-secondary); }
.fact-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin: 16px 0; }
article { padding: 14px; border-left: 4px solid; border-radius: 6px; background: var(--el-fill-color-light); }
article strong { text-transform: uppercase; font-size: 11px; }
article.fact { border-color: var(--el-color-success); }
article.inference { border-color: var(--el-color-warning); }
article.unknown { border-color: var(--el-color-info); }
.postmortem-columns { display: grid; grid-template-columns: 2fr 1fr; gap: 18px; }
li { margin-bottom: 7px; }
code { overflow-wrap: anywhere; }
@media (max-width: 760px) { .postmortem-heading, .postmortem-columns { display: block; } }
</style>
