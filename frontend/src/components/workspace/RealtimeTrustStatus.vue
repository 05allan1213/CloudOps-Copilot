<script setup lang="ts">
import { computed } from "vue";

import {
  realtimeTrustDefinition,
  type RealtimeTrustState,
} from "./workspacePresentation";

const props = withDefaults(defineProps<{
  state: RealtimeTrustState;
  cursor?: string;
  lastContinuousAt?: string;
  detail?: string;
  newItems?: number;
}>(), {
  cursor: "",
  lastContinuousAt: "",
  detail: "",
  newItems: 0,
});

const emit = defineEmits<{
  loadNew: [];
}>();

const definition = computed(() => realtimeTrustDefinition(props.state));
const newItemsLabel = computed(() => `${props.newItems.toLocaleString("zh-CN")} 条新内容`);
</script>

<template>
  <section
    class="realtime-trust-status"
    :class="{ 'is-live': definition.live, 'is-animated': definition.animated }"
    role="status"
    aria-live="polite"
  >
    <div class="realtime-trust-summary">
      <UIcon
        :name="definition.icon"
        class="realtime-trust-icon"
        aria-hidden="true"
      />
      <UBadge
        :color="definition.color"
        variant="soft"
        :label="definition.label"
      />
      <div>
        <strong>{{ definition.claim }}</strong>
        <span v-if="detail">{{ detail }}</span>
      </div>
    </div>
    <div
      v-if="cursor || lastContinuousAt"
      class="realtime-trust-identity"
    >
      <span v-if="cursor">cursor {{ cursor }}</span>
      <time
        v-if="lastContinuousAt"
        :datetime="lastContinuousAt"
      >连续至 {{ lastContinuousAt }}</time>
    </div>
    <UButton
      v-if="newItems > 0"
      color="primary"
      variant="soft"
      icon="i-lucide-list-plus"
      :label="`${newItemsLabel}，立即加载`"
      @click="emit('loadNew')"
    />
  </section>
</template>

<style scoped>
.realtime-trust-status {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-3);
  padding: var(--co-space-2) var(--co-space-3);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
}

.realtime-trust-summary {
  display: flex;
  min-width: 0;
  flex: 1 1 360px;
  align-items: center;
  gap: var(--co-space-3);
}

.realtime-trust-summary strong,
.realtime-trust-summary span { display: block; }
.realtime-trust-summary strong { font-size: 12px; }
.realtime-trust-summary span { margin-top: 2px; color: var(--co-text-muted); font-size: 11px; }
.realtime-trust-icon { flex: 0 0 auto; color: var(--co-text-secondary); }
.realtime-trust-identity {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: var(--co-space-2);
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 10px;
}

.is-animated .realtime-trust-icon { animation: realtime-trust-spin 0.8s linear infinite; }
@keyframes realtime-trust-spin { to { transform: rotate(360deg); } }

@media (prefers-reduced-motion: reduce) {
  .is-animated .realtime-trust-icon { animation: none; }
}
</style>
