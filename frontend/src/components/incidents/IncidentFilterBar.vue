<script setup lang="ts">
import { computed } from "vue";

import { incidentStatuses, incidentStatusLabel, severityLabel } from "../../models/incidents";
import type { IncidentSeverity, IncidentStatus } from "../../types/incidents";

const props = defineProps<{
  status?: IncidentStatus;
  severity?: IncidentSeverity;
  service?: string;
  loading?: boolean;
}>();

const emit = defineEmits<{
  "update:status": [value: IncidentStatus | undefined];
  "update:severity": [value: IncidentSeverity | undefined];
  "update:service": [value: string | undefined];
  apply: [];
  reset: [];
}>();

const severities: IncidentSeverity[] = ["critical", "warning", "info", "unknown"];
const statusModel = computed({
  get: () => props.status ?? "",
  set: (value: string) => emit("update:status", (value || undefined) as IncidentStatus | undefined),
});
const severityModel = computed({
  get: () => props.severity ?? "",
  set: (value: string) => emit("update:severity", (value || undefined) as IncidentSeverity | undefined),
});
const serviceModel = computed({
  get: () => props.service ?? "",
  set: (value: string) => emit("update:service", value.trimStart() || undefined),
});
const hasFilters = computed(() => Boolean(props.status || props.severity || props.service));
</script>

<template>
  <form
    class="filter-bar"
    aria-labelledby="incident-filters-title"
    @submit.prevent="$emit('apply')"
  >
    <div class="filter-heading">
      <div>
        <h2 id="incident-filters-title">
          Filter Incidents
        </h2>
        <p id="incident-filter-help">
          Status, severity, and exact service are the server-supported filters.
        </p>
      </div>
      <span class="filter-contract">URL-synced</span>
    </div>

    <div class="filter-grid">
      <div class="filter-field">
        <label for="incident-status-filter">Status</label>
        <el-select
          id="incident-status-filter"
          v-model="statusModel"
          name="status"
          aria-label="Incident status"
          clearable
          placeholder="Any status…"
        >
          <el-option
            v-for="statusOption in incidentStatuses"
            :key="statusOption"
            :label="incidentStatusLabel(statusOption)"
            :value="statusOption"
          />
        </el-select>
      </div>

      <div class="filter-field">
        <label for="incident-severity-filter">Severity</label>
        <el-select
          id="incident-severity-filter"
          v-model="severityModel"
          name="severity"
          aria-label="Incident severity"
          clearable
          placeholder="Any severity…"
        >
          <el-option
            v-for="severityOption in severities"
            :key="severityOption"
            :label="severityLabel(severityOption)"
            :value="severityOption"
          />
        </el-select>
      </div>

      <div class="filter-field filter-field--service">
        <label for="incident-service-filter">Exact service</label>
        <el-input
          id="incident-service-filter"
          v-model="serviceModel"
          name="service"
          maxlength="255"
          clearable
          autocomplete="off"
          :spellcheck="false"
          aria-describedby="incident-filter-help"
          placeholder="For example, checkout-api…"
        />
      </div>
    </div>

    <div class="filter-actions">
      <el-button
        native-type="submit"
        type="primary"
        :loading="loading"
      >
        {{ loading ? "Applying…" : "Apply Filters" }}
      </el-button>
      <el-button
        native-type="button"
        :disabled="!hasFilters || loading"
        @click="$emit('reset')"
      >
        Clear Filters
      </el-button>
    </div>
  </form>
</template>

<style scoped>
.filter-bar {
  display: grid;
  gap: var(--co-space-4);
  min-width: 0;
  padding: var(--co-space-5);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.filter-heading,
.filter-actions {
  display: flex;
  align-items: center;
}

.filter-heading {
  justify-content: space-between;
  gap: var(--co-space-4);
}

.filter-heading h2,
.filter-heading p {
  margin: 0;
}

.filter-heading h2 {
  font-size: 16px;
}

.filter-heading p {
  margin-top: var(--co-space-1);
  color: var(--co-text-secondary);
  font-size: 13px;
}

.filter-contract {
  flex: 0 0 auto;
  padding: 3px 8px;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
  font-size: 11px;
  font-weight: 650;
  text-transform: uppercase;
}

.filter-grid {
  display: grid;
  grid-template-columns: minmax(160px, 0.8fr) minmax(160px, 0.8fr) minmax(220px, 1.4fr);
  gap: var(--co-space-3);
  min-width: 0;
}

.filter-field {
  display: grid;
  min-width: 0;
  gap: var(--co-space-2);
}

.filter-field label {
  color: var(--co-text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.filter-actions {
  flex-wrap: wrap;
  gap: var(--co-space-2);
}

.filter-actions :deep(.el-button) {
  min-height: 44px;
}

@media (max-width: 900px) {
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-field--service {
    grid-column: 1 / -1;
  }
}

@media (max-width: 560px) {
  .filter-heading {
    align-items: flex-start;
  }

  .filter-contract {
    display: none;
  }

  .filter-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .filter-field--service {
    grid-column: auto;
  }

  .filter-actions > :deep(.el-button) {
    flex: 1 1 auto;
  }
}
</style>
