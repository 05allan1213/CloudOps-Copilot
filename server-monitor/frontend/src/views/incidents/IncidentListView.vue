<script setup lang="ts">
import { onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";

import { incidentDetailPath, incidentStatuses, severityLabel } from "../../models/incidents";
import { useIncidentList } from "../../composables/incidents/useIncidentList";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "../../components/incidents/IncidentStatusBadge.vue";

const route = useRoute();
const router = useRouter();
const { filters, items, total, state, error, page, load, syncURLAndLoad, reset } = useIncidentList(router, route.query);

onMounted(load);
</script>

<template>
  <main class="incident-list-view">
    <header>
      <div>
        <p class="eyebrow">
          Server-authoritative incident operations
        </p><h1>Incident Workbench</h1><p>Trace persisted signals, investigation, remediation, delivery, verification and postmortem facts in one place.</p>
      </div>
    </header>
    <section
      class="filters"
      aria-labelledby="incident-filters-title"
    >
      <h2 id="incident-filters-title">
        Filters
      </h2>
      <div class="filter-grid">
        <el-input
          v-model="filters.q"
          aria-label="Search incidents"
          maxlength="120"
          clearable
          placeholder="Search bounded title, service, namespace or workload"
          @keyup.enter="syncURLAndLoad(true)"
        />
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
          maxlength="120"
          clearable
          placeholder="Service"
        />
        <el-input
          v-model="filters.environment"
          aria-label="Environment"
          maxlength="120"
          clearable
          placeholder="Environment"
        />
        <el-input
          v-model="filters.namespace"
          aria-label="Namespace"
          maxlength="120"
          clearable
          placeholder="Namespace"
        />
        <el-input
          v-model="filters.workload"
          aria-label="Workload"
          maxlength="120"
          clearable
          placeholder="Workload"
        />
        <el-input
          v-model="filters.created_from"
          aria-label="Created from UTC"
          clearable
          placeholder="From RFC3339 UTC"
        />
        <el-input
          v-model="filters.created_to"
          aria-label="Created to UTC"
          clearable
          placeholder="To RFC3339 UTC"
        />
      </div>
      <div class="filter-actions">
        <el-button
          type="primary"
          @click="syncURLAndLoad(true)"
        >
          Apply filters
        </el-button><el-button @click="reset">
          Reset
        </el-button>
      </div>
    </section>
    <div
      v-if="state === 'loading'"
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
      Permission denied. Incident listing follows the existing RBAC policy.
    </div>
    <div
      v-else-if="state === 'error'"
      role="alert"
      class="state-message state-message--error"
    >
      {{ error }} <el-button @click="load">
        Retry
      </el-button>
    </div>
    <div
      v-else-if="state === 'empty'"
      role="status"
      class="state-message"
    >
      No incidents match these filters.
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
              <span class="severity">{{ severityLabel(incident.severity) }}</span><IncidentStatusBadge :status="incident.status" />
            </div>
            <h3>{{ incident.title }}</h3><p>{{ incident.triggering_summary || "Unknown triggering signal" }}</p>
            <dl><div><dt>Service / environment</dt><dd>{{ incident.service || "Unknown" }} / {{ incident.environment || "Unknown" }}</dd></div><div><dt>Namespace / workload</dt><dd>{{ incident.namespace || "Unknown" }} / {{ incident.workload_name || "Unknown" }}</dd></div><div><dt>Updated (UTC)</dt><dd>{{ formatIncidentTime(incident.updated_at) }}</dd></div><div><dt>Lifecycle summaries</dt><dd>{{ incident.summary.investigation.status }} · {{ incident.summary.approval.status }} · {{ incident.summary.delivery.status }} · {{ incident.summary.verification.status }}</dd></div><div><dt>Postmortem</dt><dd>{{ incident.summary.postmortem.status }}</dd></div></dl>
          </router-link>
        </li>
      </ul>
      <el-pagination
        v-model:current-page="page"
        :page-size="filters.page_size || 20"
        :total="total"
        layout="prev, pager, next, total"
        aria-label="Incident pagination"
        @current-change="syncURLAndLoad(false)"
      />
    </section>
  </main>
</template>

<style scoped>
.incident-list-view { display: grid; gap: 20px; }
header { padding: 24px; border-radius: 12px; color: #fff; background: linear-gradient(125deg, #172554, #312e81 55%, #0f766e); }
header h1 { margin: 4px 0; font-size: clamp(25px, 4vw, 36px); } header p { margin: 0; opacity: .82; }.eyebrow { font-size: 12px; text-transform: uppercase; }
.filters { padding: 18px; border: 1px solid var(--cloudops-border-color); border-radius: 10px; background: var(--cloudops-bg-card); }.filters h2 { margin-top: 0; font-size: 17px; }.filter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 10px; }.filter-actions { display: flex; gap: 8px; margin-top: 12px; }
.state-message { padding: 38px; text-align: center; color: var(--el-text-color-secondary); }.state-message--error { color: var(--el-color-danger); }
.incident-grid { list-style: none; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(310px, 1fr)); gap: 14px; }.incident-card { display: block; height: 100%; box-sizing: border-box; padding: 18px; color: inherit; text-decoration: none; border: 1px solid var(--cloudops-border-color); border-radius: 10px; background: var(--cloudops-bg-card); }.incident-card:hover, .incident-card:focus-visible { border-color: var(--el-color-primary); outline: 3px solid var(--el-color-primary-light-8); }.card-top { display: flex; justify-content: space-between; gap: 8px; }.severity { font-size: 12px; text-transform: uppercase; }.incident-card h3 { margin-bottom: 7px; }.incident-card > p { color: var(--el-text-color-secondary); }.incident-card dl { display: grid; gap: 8px; }.incident-card dl div { display: grid; grid-template-columns: minmax(110px, .7fr) 1fr; gap: 8px; }dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; }dd { margin: 0; overflow-wrap: anywhere; font-size: 12px; }.visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
</style>
