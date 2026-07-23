<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
  CircleCloseFilled,
  InfoFilled,
  Lock,
  Refresh,
  RemoveFilled,
  WarningFilled,
} from "@element-plus/icons-vue";

import type { LoadState } from "../../types/incidents";

type StateBlockState = Exclude<LoadState, "ready"> | "disabled";

const props = withDefaults(defineProps<{
  state: StateBlockState;
  headingLevel?: 2 | 3;
  title?: string;
  message?: string;
  requestID?: string;
  traceID?: string;
  busy?: boolean;
  primaryActionLabel?: string;
  secondaryActionLabel?: string;
}>(), {
  headingLevel: 3,
  title: "",
  message: "",
  requestID: "",
  traceID: "",
  busy: false,
  primaryActionLabel: "",
  secondaryActionLabel: "",
});

defineEmits<{
  primaryAction: [];
  secondaryAction: [];
}>();

const definitions: Record<StateBlockState, { title: string; message: string; icon: Component; tone: string }> = {
  loading: { title: "Loading", message: "Fetching the latest server projection…", icon: markRaw(Refresh), tone: "neutral" },
  empty: { title: "No incidents found", message: "No V3 incidents match the current filters.", icon: markRaw(RemoveFilled), tone: "neutral" },
  error: { title: "Incidents could not be loaded", message: "Retry the request. If it continues to fail, use the request identifiers below.", icon: markRaw(CircleCloseFilled), tone: "danger" },
  forbidden: { title: "Viewer access is required", message: "Use a GitHub identity with viewer or operator access, then retry the access check.", icon: markRaw(Lock), tone: "warning" },
  not_found: { title: "Projection not found", message: "The requested Incident projection does not exist or is no longer available.", icon: markRaw(InfoFilled), tone: "neutral" },
  unavailable: { title: "Incident projection unavailable", message: "The API cannot serve this projection right now. Retry after the dependency recovers.", icon: markRaw(WarningFilled), tone: "warning" },
  disabled: { title: "Action unavailable", message: "This action is not available in the current state.", icon: markRaw(Lock), tone: "neutral" },
};

const definition = computed(() => definitions[props.state]);
const effectiveTitle = computed(() => props.title || definition.value.title);
const effectiveMessage = computed(() => props.message || definition.value.message);
const headingTag = computed(() => `h${props.headingLevel}`);
const role = computed(() => ["error", "forbidden", "unavailable"].includes(props.state) ? "alert" : "status");
</script>

<template>
  <section
    class="state-block"
    :class="`state-block--${definition.tone}`"
    :role="role"
    :aria-live="role === 'alert' ? 'assertive' : 'polite'"
  >
    <span
      class="state-icon"
      aria-hidden="true"
    >
      <el-icon :size="22">
        <component :is="definition.icon" />
      </el-icon>
    </span>
    <div class="state-copy">
      <component :is="headingTag">
        {{ effectiveTitle }}
      </component>
      <p>{{ effectiveMessage }}</p>
      <dl
        v-if="requestID || traceID"
        class="request-identity"
      >
        <div v-if="requestID">
          <dt>Request ID</dt>
          <dd><code translate="no">{{ requestID }}</code></dd>
        </div>
        <div v-if="traceID">
          <dt>Trace ID</dt>
          <dd><code translate="no">{{ traceID }}</code></dd>
        </div>
      </dl>
      <div
        v-if="primaryActionLabel || secondaryActionLabel"
        class="state-actions"
      >
        <el-button
          v-if="primaryActionLabel"
          type="primary"
          :loading="busy"
          :disabled="busy"
          @click="$emit('primaryAction')"
        >
          {{ primaryActionLabel }}
        </el-button>
        <el-button
          v-if="secondaryActionLabel"
          :disabled="busy"
          @click="$emit('secondaryAction')"
        >
          {{ secondaryActionLabel }}
        </el-button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.state-block {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr);
  gap: var(--co-space-4);
  padding: var(--co-space-6);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.state-block--danger { border-color: var(--co-status-critical-border); }
.state-block--warning { border-color: var(--co-status-warning-border); }

.state-icon {
  display: grid;
  width: 40px;
  height: 40px;
  place-items: center;
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
}

.state-block--danger .state-icon { color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.state-block--warning .state-icon { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }

.state-copy,
.state-copy :is(h2, h3),
.state-copy p,
.request-identity {
  min-width: 0;
  margin: 0;
}

.state-copy :is(h2, h3) {
  color: var(--co-text-primary);
  font-size: 16px;
}

.state-copy p {
  margin-top: var(--co-space-1);
  color: var(--co-text-secondary);
}

.request-identity {
  display: grid;
  gap: var(--co-space-2);
  margin-top: var(--co-space-4);
}

.request-identity div {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: var(--co-space-2);
}

.request-identity dt {
  color: var(--co-text-muted);
  font-size: 12px;
}

.request-identity dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}

.state-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--co-space-2);
  margin-top: var(--co-space-4);
}

.state-actions :deep(.el-button) {
  min-height: 44px;
}

@media (max-width: 560px) {
  .state-block {
    grid-template-columns: minmax(0, 1fr);
    padding: var(--co-space-5);
  }
}
</style>
