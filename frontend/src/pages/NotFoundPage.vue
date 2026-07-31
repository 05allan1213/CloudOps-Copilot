<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

const route = useRoute();
const unknownPath = computed(() => route.fullPath);
</script>

<template>
  <article
    class="not-found-page"
    data-testid="not-found-page"
  >
    <div
      class="not-found-code"
      aria-hidden="true"
    >
      <UIcon name="i-lucide-file-question" />
      <span>404</span>
    </div>
    <div class="not-found-copy">
      <UBadge
        color="warning"
        variant="soft"
        icon="i-lucide-route-off"
        label="未知路径"
      />
      <h1 tabindex="-1">
        页面不存在
      </h1>
      <p>请求的工作区路径不存在或已被移除。</p>
      <code>{{ unknownPath }}</code>
    </div>
    <nav
      class="not-found-actions"
      aria-label="恢复导航"
    >
      <UButton
        color="primary"
        icon="i-lucide-layout-dashboard"
        label="返回总览"
        to="/overview"
      />
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-server"
        label="查看基础设施"
        to="/infrastructure"
      />
    </nav>
  </article>
</template>

<style scoped>
.not-found-page {
  display: grid;
  width: min(100%, 760px);
  min-height: min(620px, calc(100dvh - var(--co-header-height) - (2 * var(--co-space-6))));
  margin: 0 auto;
  grid-template-columns: 180px minmax(0, 1fr);
  align-content: center;
  align-items: center;
  gap: var(--co-space-6);
}

.not-found-code {
  display: grid;
  justify-items: center;
  color: var(--co-status-warning-fg);
}
.not-found-code svg { width: 64px; height: 64px; }
.not-found-code span {
  font-family: var(--co-font-mono);
  font-size: 42px;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
}
.not-found-copy { min-width: 0; }
.not-found-copy h1 { margin: var(--co-space-3) 0 var(--co-space-2); font-size: 28px; line-height: 1.25; }
.not-found-copy p { margin: 0; color: var(--co-text-secondary); }
.not-found-copy code {
  display: block;
  max-width: 100%;
  margin-top: var(--co-space-3);
  overflow: hidden;
  color: var(--co-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--co-font-mono);
  font-size: 11px;
}
.not-found-actions {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  grid-column: 2;
  gap: var(--co-space-2);
}

@media (max-width: 760px) {
  .not-found-page { grid-template-columns: minmax(0, 1fr); align-content: start; padding-top: var(--co-space-8); }
  .not-found-code { justify-items: start; }
  .not-found-actions { grid-column: 1; }
}
</style>
