<script setup lang="ts">
defineProps<{
  title: string;
  subtitle?: string;
}>();
</script>

<template>
  <div class="page-header">
    <div class="page-header-info">
      <h2 class="page-header-title">
        {{ title }}
      </h2>
      <p
        v-if="subtitle"
        class="page-header-subtitle"
      >
        {{ subtitle }}
      </p>
    </div>
    <div class="page-header-actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
  gap: 16px;
}

.page-header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.page-header-title {
  font-size: 20px;
  font-weight: 700;
  color: var(--cloudops-text-primary);
  margin: 0;
}

.page-header-subtitle {
  font-size: 13px;
  color: var(--cloudops-text-muted);
  margin: 0;
}

.page-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
