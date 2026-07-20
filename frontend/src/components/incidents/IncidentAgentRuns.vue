<script setup lang="ts">
import type { InvestigationDTO, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; investigation: InvestigationDTO | null }>();
</script>

<template>
  <IncidentSectionShell
    id="investigation"
    title="Agent Investigation"
    :state="state"
    :error="error"
    empty-text="No AgentRun has been persisted."
  >
    <template v-if="investigation?.runs[0]">
      <div class="run-header">
        <div><strong>Run {{ investigation.runs[0].id }}</strong><p>Attempt {{ investigation.runs[0].attempt }} · {{ investigation.runs[0].used_steps }}/{{ investigation.runs[0].max_steps }} steps · {{ investigation.runs[0].used_tool_calls }}/{{ investigation.runs[0].max_tool_calls }} tools</p></div>
        <IncidentStatusBadge
          :status="investigation.runs[0].status"
          :label="investigation.runs[0].status"
        />
      </div>
      <el-alert
        v-if="investigation.runs[0].termination_reason"
        :title="`Termination: ${investigation.runs[0].termination_reason}`"
        type="warning"
        :closable="false"
      />
      <div
        v-if="investigation.runs[0].diagnosis"
        class="diagnosis"
      >
        <h3>Persisted diagnosis</h3>
        <p>{{ investigation.runs[0].diagnosis.summary || "Unknown" }}</p>
        <div class="diagnosis-columns">
          <div>
            <strong>Confirmed facts</strong><ul>
              <li
                v-for="fact in investigation.runs[0].diagnosis.confirmed_facts"
                :key="fact.statement"
              >
                {{ fact.statement }} <small>Evidence: {{ fact.evidence_ids.join(', ') || 'Unknown' }}</small>
              </li>
            </ul>
          </div>
          <div>
            <strong>Hypotheses</strong><ul>
              <li
                v-for="item in investigation.runs[0].diagnosis.hypotheses"
                :key="item.statement"
              >
                {{ item.statement }} <small>{{ Math.round(item.confidence * 100) }}% persisted confidence</small>
              </li>
            </ul>
          </div>
          <div>
            <strong>Unknowns</strong><ul>
              <li
                v-for="item in investigation.runs[0].diagnosis.unknowns"
                :key="item"
              >
                {{ item }}
              </li>
            </ul>
          </div>
        </div>
        <p class="reasoning-note">
          Private model reasoning is never displayed. Only the validated, persisted diagnosis contract appears here.
        </p>
      </div>
      <h3>Steps</h3>
      <el-table
        :data="investigation.steps"
        aria-label="Agent investigation steps"
      >
        <el-table-column
          prop="sequence"
          label="#"
          width="55"
        />
        <el-table-column
          prop="type"
          label="Step type"
          min-width="150"
        />
        <el-table-column
          prop="typed_tool"
          label="Typed tool"
          min-width="150"
        >
          <template #default="{ row }">
            {{ row.typed_tool || "—" }}
          </template>
        </el-table-column>
        <el-table-column
          prop="status"
          label="Status"
          width="105"
        />
        <el-table-column
          prop="evidence_id"
          label="Evidence"
          min-width="180"
        >
          <template #default="{ row }">
            {{ row.evidence_id || "—" }}
          </template>
        </el-table-column>
        <el-table-column
          label="Started (UTC)"
          min-width="180"
        >
          <template #default="{ row }">
            {{ formatIncidentTime(row.started_at) }}
          </template>
        </el-table-column>
      </el-table>
    </template>
  </IncidentSectionShell>
</template>

<style scoped>
.run-header { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
.run-header p { margin: 5px 0 0; color: var(--el-text-color-secondary); }
.diagnosis { margin: 18px 0; padding: 16px; background: var(--el-fill-color-light); border-radius: 8px; }
.diagnosis h3 { margin-top: 0; }
.diagnosis-columns { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 16px; }
ul { padding-left: 18px; }
li { margin-bottom: 6px; }
small { display: block; color: var(--el-text-color-secondary); }
.reasoning-note { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
