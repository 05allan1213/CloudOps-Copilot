<template>
  <div class="not-found-page">
    <h1 class="visually-hidden">
      页面不存在
    </h1>
    <el-result
      icon="warning"
      title="404"
      sub-title="找不到请求的页面。"
    >
      <template #extra>
        <RouterLink
          class="not-found-action"
          to="/overview"
        >
          返回总览
        </RouterLink>
      </template>
    </el-result>
  </div>
</template>

<style scoped>
.not-found-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}

.not-found-action {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  padding: 0 var(--co-space-4);
  border: 1px solid var(--co-action-primary);
  border-radius: var(--co-radius-control);
  color: var(--co-text-on-action);
  background: var(--co-action-primary);
  font-weight: 700;
  text-decoration: none;
  transition: background-color var(--co-motion-fast) var(--co-ease-out), border-color var(--co-motion-fast) var(--co-ease-out), box-shadow var(--co-motion-fast) var(--co-ease-out);
}

.not-found-action:hover {
  border-color: var(--co-action-hover);
  background: var(--co-action-hover);
}

.not-found-action:focus-visible {
  outline: 3px solid var(--co-focus-ring);
  outline-offset: 3px;
}
</style>
