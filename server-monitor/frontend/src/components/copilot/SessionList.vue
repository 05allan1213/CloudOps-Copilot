<script setup lang="ts">
import { ElMessageBox } from "element-plus";
import { Plus, Delete } from "@element-plus/icons-vue";

import type { CopilotSession } from "../../types";
import { formatTimeShort } from "../../utils/format";

defineProps<{
  sessions: CopilotSession[];
  activeSessionId: string;
  loadingSessions: boolean;
}>();

const emit = defineEmits<{
  select: [sessionId: string];
  new: [];
  delete: [sessionId: string];
}>();

async function handleConfirmDelete(sessionId: string) {
  try {
    await ElMessageBox.confirm("确认删除该会话？此操作不可撤销。", "删除确认", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
    emit("delete", sessionId);
  } catch {
    // cancelled
  }
}
</script>

<template>
  <div class="session-panel">
    <div class="session-panel-header">
      <div>
        <h2>Copilot</h2>
        <span>{{ loadingSessions ? "同步中" : `${sessions.length} 个会话` }}</span>
      </div>
      <el-button
        :icon="Plus"
        size="small"
        circle
        @click="emit('new')"
      />
    </div>

    <div class="session-list">
      <div
        v-for="session in sessions"
        :key="session.id"
        class="session-item"
        :class="{ active: session.id === activeSessionId }"
        @click="emit('select', session.id)"
      >
        <span class="session-title">{{ session.title || session.id }}</span>
        <small>{{ formatTimeShort(session.updated_at) }}</small>
      </div>
      <el-empty
        v-if="!loadingSessions && sessions.length === 0"
        description="暂无会话"
        :image-size="48"
      />
    </div>

    <div
      v-if="activeSessionId"
      class="session-footer"
    >
      <el-button
        :icon="Delete"
        type="danger"
        text
        size="small"
        @click="handleConfirmDelete(activeSessionId)"
      >
        删除当前会话
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.session-panel {
  display: flex;
  flex-direction: column;
  min-height: 520px;
  border: 1px solid var(--cloudops-border-color);
  border-radius: var(--cloudops-radius-md);
  background: var(--cloudops-bg-card);
}

.session-panel-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px;
  border-bottom: 1px solid var(--cloudops-border-color);
}

.session-panel-header h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.session-panel-header span {
  display: block;
  margin-top: 4px;
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

.session-list {
  flex: 1;
  overflow: auto;
  padding: 8px;
}

.session-item {
  display: grid;
  gap: 4px;
  padding: 10px 12px;
  border-radius: var(--cloudops-radius-sm);
  cursor: pointer;
  transition: background 0.15s;
}

.session-item:hover {
  background: var(--el-fill-color);
}

.session-item.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.session-title {
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-item small {
  color: var(--el-text-color-placeholder);
  font-size: 11px;
}

.session-footer {
  padding: 8px 12px;
  border-top: 1px solid var(--cloudops-border-color);
}

@media (max-width: 860px) {
  .session-panel {
    min-height: auto;
  }
  .session-list {
    max-height: 220px;
  }
}
</style>
