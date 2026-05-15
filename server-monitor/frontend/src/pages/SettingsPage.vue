<script setup lang="ts">
import { Bell, ChatDotRound, User } from "@element-plus/icons-vue";
import { RouterLink } from "vue-router";

import PageHeader from "../components/common/PageHeader.vue";

const settingsItems = [
  {
    to: "/settings/alert-rules",
    icon: Bell,
    title: "告警规则",
    desc: "维护 MySQL 中的自定义 Prometheus 告警规则，并触发同步。",
  },
  {
    to: "/settings/channels",
    icon: ChatDotRound,
    title: "通知渠道",
    desc: "维护 Webhook 通知地址，并执行受限连通性测试。",
  },
  {
    to: "/settings/users",
    icon: User,
    title: "用户管理",
    desc: "管理系统用户和角色分配。",
  },
];
</script>

<template>
  <section class="settings-page">
    <PageHeader title="设置" subtitle="管理系统的业务配置" />

    <el-row :gutter="16">
      <el-col
        v-for="item in settingsItems"
        :key="item.to"
        :xs="24"
        :sm="12"
        :md="8"
      >
        <RouterLink :to="item.to" class="settings-card-link">
          <el-card shadow="hover" class="settings-card">
            <div class="settings-card-body">
              <div class="settings-card-icon">
                <el-icon :size="24"><component :is="item.icon" /></el-icon>
              </div>
              <div class="settings-card-text">
                <span class="settings-card-title">{{ item.title }}</span>
                <span class="settings-card-desc">{{ item.desc }}</span>
              </div>
            </div>
          </el-card>
        </RouterLink>
      </el-col>
    </el-row>
  </section>
</template>

<style scoped>
.settings-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.settings-card-link {
  text-decoration: none;
  display: block;
  margin-bottom: 16px;
}

.settings-card {
  cursor: pointer;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.settings-card:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

:global(html.light) .settings-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
}

.settings-card :deep(.el-card__body) {
  padding: 20px;
}

.settings-card-body {
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.settings-card-icon {
  width: 48px;
  height: 48px;
  border-radius: var(--cloudops-radius-md);
  background: rgba(59, 130, 246, 0.1);
  color: var(--el-color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.settings-card-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.settings-card-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.settings-card-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
</style>
