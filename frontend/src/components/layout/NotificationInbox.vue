<script setup lang="ts">
import type { OwnerNotification } from "../../api/notifications";
import { contextLocation } from "../../utils/contextLink";

defineProps<{
  items: OwnerNotification[];
  unreadCount: number;
  loading: boolean;
  error: string;
  streamState: "connected" | "reconnecting" | "stopped";
}>();

const emit = defineEmits<{
  refresh: [];
  read: [notification: OwnerNotification];
  readAll: [];
  navigate: [];
}>();

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "short",
});

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : dateFormatter.format(date);
}

function isNavigable(item: OwnerNotification): boolean {
  return contextLocation(item.context_link) !== null;
}

function openNotification(item: OwnerNotification) {
  emit("read", item);
  emit("navigate");
}

function severityColor(severity: OwnerNotification["severity"]): "error" | "warning" | "neutral" {
  if (severity === "P1" || severity === "P2") return "error";
  if (severity === "P3") return "warning";
  return "neutral";
}

function streamLabel(state: "connected" | "reconnecting" | "stopped"): string {
  if (state === "connected") return "实时连接正常";
  if (state === "reconnecting") return "实时连接正在恢复";
  return "实时连接已停止";
}

function streamColor(state: "connected" | "reconnecting" | "stopped"): "success" | "warning" | "neutral" {
  if (state === "connected") return "success";
  if (state === "reconnecting") return "warning";
  return "neutral";
}
</script>

<template>
  <section
    class="notification-inbox"
    aria-labelledby="notification-inbox-title"
  >
    <header class="inbox-heading">
      <div>
        <h2 id="notification-inbox-title">
          通知收件箱
        </h2>
        <p>
          <UBadge
            color="neutral"
            variant="soft"
            size="sm"
            :label="`${unreadCount} 条未读`"
          />
        </p>
      </div>
      <div class="inbox-actions">
        <UTooltip text="刷新通知">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新通知"
            :loading="loading"
            @click="emit('refresh')"
          />
        </UTooltip>
        <UButton
          color="neutral"
          variant="soft"
          icon="i-lucide-check-check"
          label="全部已读"
          :disabled="loading || unreadCount === 0"
          @click="emit('readAll')"
        />
      </div>
    </header>

    <UBadge
      class="stream-state"
      :color="streamColor(streamState)"
      variant="soft"
      :icon="streamState === 'connected' ? 'i-lucide-radio' : streamState === 'reconnecting' ? 'i-lucide-refresh-cw' : 'i-lucide-circle-pause'"
      :label="streamLabel(streamState)"
      role="status"
      aria-live="polite"
    />

    <UAlert
      v-if="error"
      color="error"
      variant="soft"
      icon="i-lucide-triangle-alert"
      title="通知读取失败"
      :description="error"
      role="alert"
    >
      <template #actions>
        <UButton
          color="error"
          variant="soft"
          icon="i-lucide-rotate-cw"
          label="重试"
          @click="emit('refresh')"
        />
      </template>
    </UAlert>

    <div
      v-else-if="loading && items.length === 0"
      class="inbox-loading"
      role="status"
      aria-live="polite"
    >
      <span class="visually-hidden">正在读取通知</span>
      <USkeleton
        v-for="index in 3"
        :key="index"
        class="notification-skeleton"
      />
    </div>

    <div
      v-else-if="items.length === 0"
      class="inbox-empty"
    >
      <UIcon
        name="i-lucide-inbox"
        aria-hidden="true"
      />
      <strong>暂无通知</strong>
      <span>当前没有需要 Owner 处理的事件。</span>
    </div>

    <ol
      v-else
      class="notification-list"
    >
      <li
        v-for="item in items"
        :key="item.id"
        :class="{ 'is-unread': !item.read }"
      >
        <div class="notification-meta">
          <UBadge
            :color="severityColor(item.severity)"
            variant="soft"
            size="sm"
            :label="item.severity"
          />
          <span class="source">{{ item.source_type }} · {{ item.source_state }}</span>
          <time :datetime="item.created_at">{{ formatTime(item.created_at) }}</time>
        </div>
        <p>{{ item.reason }}</p>
        <div class="notification-footer">
          <UButton
            v-if="isNavigable(item)"
            color="primary"
            variant="link"
            trailing-icon="i-lucide-arrow-right"
            label="打开上下文"
            :to="{ path: item.context_link.path, query: item.context_link.query }"
            @click="openNotification(item)"
          />
          <span
            v-else
            class="unavailable-link"
          >上下文不可用</span>
          <UTooltip
            v-if="!item.read"
            text="标记为已读"
          >
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-check"
              square
              aria-label="标记为已读"
              @click="emit('read', item)"
            />
          </UTooltip>
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.notification-inbox {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
}

.inbox-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.inbox-heading h2 { margin: 0; font-size: 16px; }
.inbox-heading p { margin: var(--co-space-1) 0 0; }
.inbox-actions, .notification-footer, .notification-meta { display: flex; align-items: center; }
.inbox-actions { gap: var(--co-space-2); }
.stream-state { width: fit-content; }

.inbox-loading { display: grid; gap: var(--co-space-3); }
.notification-skeleton { height: 104px; border-radius: var(--co-radius-panel); }

.inbox-empty {
  display: grid;
  min-height: 180px;
  place-content: center;
  justify-items: center;
  gap: var(--co-space-2);
  padding: var(--co-space-6);
  border-block: 1px solid var(--co-border-default);
  color: var(--co-text-muted);
  text-align: center;
}

.inbox-empty svg { width: 24px; height: 24px; }
.inbox-empty strong { color: var(--co-text-primary); }
.notification-list { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.notification-list li { display: grid; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.notification-list li.is-unread { border-left: 3px solid var(--co-action-primary); background: var(--co-bg-subtle); }
.notification-list p { margin: 0; color: var(--co-text-primary); overflow-wrap: anywhere; }
.notification-meta { min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 11px; }
.notification-meta time { margin-left: auto; font-variant-numeric: tabular-nums; }
.source { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.notification-footer { justify-content: space-between; gap: var(--co-space-3); }
.unavailable-link { color: var(--co-text-muted); font-size: 12px; }
</style>
