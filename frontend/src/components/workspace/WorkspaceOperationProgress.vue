<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(defineProps<{
  stage: string;
  elapsedSeconds: number;
  description?: string;
  cancellable?: boolean;
  cancelling?: boolean;
}>(), {
  description: "",
  cancellable: false,
  cancelling: false,
});

const elapsedLabel = computed(() => {
  const total = Math.max(0, Math.floor(props.elapsedSeconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  return [hours, minutes, seconds].map((value) => String(value).padStart(2, "0")).join(":");
});

const emit = defineEmits<{
  cancel: [];
}>();
</script>

<template>
  <section
    class="workspace-operation-progress"
    role="status"
    aria-live="polite"
  >
    <div class="workspace-operation-summary">
      <UIcon
        name="i-lucide-loader-circle"
        class="workspace-operation-icon"
        aria-hidden="true"
      />
      <div>
        <span>长操作阶段</span>
        <strong>{{ stage }}</strong>
        <p v-if="description">
          {{ description }}
        </p>
      </div>
    </div>
    <div class="workspace-operation-meta">
      <span>已运行 <data :value="elapsedSeconds">{{ elapsedLabel }}</data></span>
      <UButton
        v-if="cancellable"
        color="neutral"
        variant="outline"
        icon="i-lucide-circle-stop"
        :label="cancelling ? '正在取消' : '取消操作'"
        :loading="cancelling"
        :disabled="cancelling"
        @click="emit('cancel')"
      />
    </div>
  </section>
</template>

<style scoped>
.workspace-operation-progress {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.workspace-operation-summary {
  display: grid;
  min-width: 0;
  flex: 1 1 320px;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: var(--co-space-3);
}

.workspace-operation-summary span,
.workspace-operation-summary strong { display: block; }
.workspace-operation-summary span { color: var(--co-text-muted); font-size: 11px; }
.workspace-operation-summary strong { margin-top: 2px; font-size: 13px; }
.workspace-operation-summary p {
  margin: var(--co-space-1) 0 0;
  color: var(--co-text-secondary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.workspace-operation-icon {
  color: var(--co-status-info-fg);
  animation: workspace-operation-spin 0.8s linear infinite;
}

.workspace-operation-meta {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--co-space-3);
  color: var(--co-text-muted);
  font-size: 11px;
}

.workspace-operation-meta data {
  color: var(--co-text-primary);
  font-family: var(--co-font-mono);
  font-variant-numeric: tabular-nums;
}

@keyframes workspace-operation-spin { to { transform: rotate(360deg); } }

@media (prefers-reduced-motion: reduce) {
  .workspace-operation-icon { animation: none; }
}
</style>
