<template>
  <section class="settings-page">
    <header class="page-header">
      <div>
        <h2>设置</h2>
        <p>管理第四阶段的业务配置。</p>
      </div>
    </header>

    <div class="settings-grid">
      <RouterLink class="settings-item" to="/settings/alert-rules">
        <span class="item-title">告警规则</span>
        <span class="item-desc">维护 MySQL 中的自定义 Prometheus 告警规则，并触发同步。</span>
      </RouterLink>
      <RouterLink class="settings-item" to="/settings/channels">
        <span class="item-title">通知渠道</span>
        <span class="item-desc">维护 Webhook 通知地址，并执行受限连通性测试。</span>
      </RouterLink>
      <RouterLink class="settings-item" to="/settings/users">
        <span class="item-title">用户管理</span>
        <span class="item-desc">管理系统用户和角色分配。</span>
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.settings-page {
  display: grid;
  gap: 1rem;
}

.page-header h2 {
  font-size: 1.25rem;
  margin: 0;
}

.page-header p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin-top: 0.3rem;
}

.settings-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1rem;
}

.settings-item {
  display: grid;
  gap: 0.5rem;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
  transition: border-color 0.15s, transform 0.15s;
}

.settings-item:hover {
  border-color: var(--accent);
  transform: translateY(-1px);
}

.item-title {
  color: var(--text-primary);
  font-size: 0.98rem;
  font-weight: 800;
}

.item-desc {
  color: var(--text-muted);
  font-size: 0.82rem;
  line-height: 1.55;
}
</style>
