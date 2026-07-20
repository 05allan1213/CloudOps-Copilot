<script setup lang="ts">
import { onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";

import IncidentStatusBadge from "../../components/incidents/IncidentStatusBadge.vue";
import { useIncidentList } from "../../composables/incidents/useIncidentList";
import { incidentDetailPath, incidentStatuses, severityLabel } from "../../models/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";

const route = useRoute();
const router = useRouter();
const { filters, items, nextCursor, state, error, load, loadMore, syncURLAndLoad, reset } = useIncidentList(router, route.query);

onMounted(() => load(false));
</script>

<template>
  <main class="incident-list-view">
    <header>
      <div>
        <p class="eyebrow">
          MySQL-authoritative V3 operations
        </p>
        <h1>Incident Workbench</h1>
        <p>Read bounded Incident projections and follow the durable investigation, delivery and recovery chain.</p>
      </div>
    </header>
    <section
      class="filters"
      aria-labelledby="incident-filters-title"
    >
      <h2 id="incident-filters-title">
        Server-supported filters
      </h2>
      <div class="filter-grid">
        <el-select
          v-model="filters.status"
          aria-label="Incident status"
          clearable
          placeholder="Status"
        >
          <el-option
            v-for="status in incidentStatuses"
            :key="status"
            :label="status.replace(/_/g, ' ')"
            :value="status"
          />
        </el-select>
        <el-select
          v-model="filters.severity"
          aria-label="Severity"
          clearable
          placeholder="Severity"
        >
          <el-option
            v-for="severity in ['critical', 'warning', 'info', 'unknown']"
            :key="severity"
            :label="severity"
            :value="severity"
          />
        </el-select>
        <el-input
          v-model="filters.service"
          aria-label="Service"
          maxlength="255"
          clearable
          placeholder="Exact service name"
          @keyup.enter="syncURLAndLoad"
        />
      </div>
      <div class="filter-actions">
        <el-button
          type="primary"
          @click="syncURLAndLoad"
        >
          Apply filters
        </el-button>
        <el-button @click="reset">
          Reset
        </el-button>
      </div>
    </section>
    <div
      v-if="state === 'loading' && items.length === 0"
      role="status"
      aria-live="polite"
      class="state-message"
    >
      Loading incidents…
    </div>
    <div
      v-else-if="state === 'forbidden'"
      role="alert"
      class="state-message"
    >
      Viewer access is required.
    </div>
    <div
      v-else-if="state === 'error' || state === 'unavailable'"
      role="alert"
      class="state-message state-message--error"
    >
      {{ error }} <el-button @click="load(false)">
        Retry
      </el-button>
    </div>
    <div
      v-else-if="state === 'empty'"
      role="status"
      class="state-message"
    >
      No V3 incidents match these filters.
    </div>
    <section
      v-else
      aria-labelledby="incident-results-title"
    >
      <h2
        id="incident-results-title"
        class="visually-hidden"
      >
        Incident results
      </h2>
      <ul class="incident-grid">
        <li
          v-for="incident in items"
          :key="incident.id"
        >
          <router-link
            :to="incidentDetailPath(incident.id)"
            class="incident-card"
          >
            <div class="card-top">
              <span class="severity">{{ severityLabel(incident.severity) }}</span>
              <IncidentStatusBadge :status="incident.status" />
            </div>
            <h3>{{ incident.summary || "Incident cycle in progress" }}</h3>
            <dl>
              <div><dt>Cycle / version</dt><dd>{{ incident.cycle }} / {{ incident.version }}</dd></div>
              <div><dt>Needs attention</dt><dd>{{ incident.needs_attention ? "Yes" : "No" }}</dd></div>
              <div><dt>Blocking reason</dt><dd>{{ incident.blocking_reason_code || "None" }}</dd></div>
              <div><dt>Updated (UTC)</dt><dd>{{ formatIncidentTime(incident.updated_at) }}</dd></div>
            </dl>
          </router-link>
        </li>
      </ul>
      <el-button
        v-if="nextCursor"
        :loading="state === 'loading'"
        @click="loadMore"
      >
        Load next cursor page
      </el-button>
    </section>
  </main>
</template>

<style scoped>
.incident-list-view { display: grid; gap: 20px; }
header { padding: 24px; border-radius: 12px; color: #fff; background: linear-gradient(125deg, #172554, #312e81 55%, #0f766e); }
header h1 { margin: 4px 0; font-size: clamp(25px, 4vw, 36px); }
header p { margin: 0; opacity: .82; }
.eyebrow { font-size: 12px; text-transform: uppercase; }
.filters { padding: 18px; border: 1px solid var(--cloudops-border-color); border-radius: 10px; background: var(--cloudops-bg-card); }
.filters h2 { margin-top: 0; font-size: 17px; }
.filter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 10px; }
.filter-actions { display: flex; gap: 8px; margin-top: 12px; }
.state-message { padding: 38px; text-align: center; color: var(--el-text-color-secondary); }
.state-message--error { color: var(--el-color-danger); }
.incident-grid { list-style: none; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(310px, 1fr)); gap: 14px; }
.incident-card { display: block; height: 100%; box-sizing: border-box; padding: 18px; color: inherit; text-decoration: none; border: 1px solid var(--cloudops-border-color); border-radius: 10px; background: var(--cloudops-bg-card); }
.incident-card:hover, .incident-card:focus-visible { border-color: var(--el-color-primary); outline: 3px solid var(--el-color-primary-light-8); }
.card-top { display: flex; justify-content: space-between; gap: 8px; }
.severity { font-size: 12px; text-transform: uppercase; }
.incident-card h3 { margin-bottom: 12px; overflow-wrap: anywhere; }
.incident-card dl { display: grid; gap: 8px; }
.incident-card dl div { display: grid; grid-template-columns: minmax(110px, .7fr) 1fr; gap: 8px; }
dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; }
dd { margin: 0; overflow-wrap: anywhere; font-size: 12px; }
.visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
</style>
