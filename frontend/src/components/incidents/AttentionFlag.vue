<script setup lang="ts">
import { CircleCheck as CircleCheckFilled, TriangleAlert as WarningFilled } from "lucide-vue-next";

defineProps<{
  active: boolean;
}>();
</script>

<template>
  <span
    class="attention-flag"
    :class="active ? 'attention-flag--active' : 'attention-flag--clear'"
  >
    <WarningFilled
      v-if="active"
      :size="14"
      aria-hidden="true"
    />
    <CircleCheckFilled
      v-else
      :size="14"
      aria-hidden="true"
    />
    {{ active ? "需要关注" : "无需关注" }}
  </span>
</template>

<style scoped>
.attention-flag {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  gap: 5px;
  color: var(--co-text-secondary);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}

.attention-flag--active {
  color: var(--co-status-warning-fg);
}

.attention-flag--clear {
  color: var(--co-status-success-fg);
}
</style>
