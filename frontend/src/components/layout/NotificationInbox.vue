<script setup lang="ts">
import { Check, CheckCheck, RefreshCw, RotateCw } from "lucide-vue-next";

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
</script>

<template>
  <section class="notification-inbox" aria-labelledby="notification-inbox-title">
    <header class="inbox-heading">
      <div>
        <h2 id="notification-inbox-title">通知收件箱</h2>
        <p>{{ unreadCount }} 条未读</p>
      </div>
      <div class="inbox-actions">
        <button
          type="button"
          class="icon-action"
          aria-label="刷新通知"
          title="刷新通知"
          :disabled="loading"
          @click="$emit('refresh')"
        >
          <RefreshCw :size="18" aria-hidden="true" />
        </button>
        <button
          type="button"
          class="command-action"
          :disabled="loading || unreadCount === 0"
          @click="$emit('readAll')"
        >
          <CheckCheck :size="17" aria-hidden="true" />
          全部已读
        </button>
      </div>
    </header>

    <p class="stream-state" :class="`is-${streamState}`" role="status" aria-live="polite">
      <span aria-hidden="true" />
      {{ streamState === "connected" ? "实时连接正常" : streamState === "reconnecting" ? "实时连接正在恢复" : "实时连接已停止" }}
    </p>

    <div v-if="error" class="inbox-error" role="alert">
      <p>{{ error }}</p>
      <button type="button" @click="$emit('refresh')"><RotateCw :size="17" aria-hidden="true" />重试</button>
    </div>

    <div v-else-if="loading && items.length === 0" class="inbox-empty" role="status" aria-live="polite">
      正在读取通知…
    </div>

    <div v-else-if="items.length === 0" class="inbox-empty">
      <strong>暂无通知</strong>
      <span>当前没有需要 Owner 处理的事件。</span>
    </div>

    <ol v-else class="notification-list">
      <li v-for="item in items" :key="item.id" :class="{ 'is-unread': !item.read }">
        <div class="notification-meta">
          <span class="severity" :class="`severity-${item.severity.toLowerCase()}`">{{ item.severity }}</span>
          <span class="source">{{ item.source_type }} · {{ item.source_state }}</span>
          <time :datetime="item.created_at">{{ formatTime(item.created_at) }}</time>
        </div>
        <p>{{ item.reason }}</p>
        <div class="notification-footer">
          <RouterLink
            v-if="isNavigable(item)"
            :to="{ path: item.context_link.path, query: item.context_link.query }"
            @click="openNotification(item)"
          >
            打开上下文
          </RouterLink>
          <span v-else class="unavailable-link">上下文不可用</span>
          <button
            v-if="!item.read"
            type="button"
            class="icon-action"
            aria-label="标记为已读"
            title="标记为已读"
            @click="$emit('read', item)"
          >
            <Check :size="17" aria-hidden="true" />
          </button>
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.notification-inbox { display: grid; gap: var(--co-space-4); min-width: 0; }
.inbox-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.inbox-heading h2 { margin: 0; font-size: 18px; }
.inbox-heading p { margin: 2px 0 0; color: var(--co-text-muted); font-size: 12px; }
.inbox-actions, .notification-footer, .notification-meta { display: flex; align-items: center; }
.inbox-actions { gap: var(--co-space-2); }
.icon-action, .command-action, .inbox-error button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: var(--co-space-2); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); cursor: pointer; }
.icon-action { width: 38px; padding: 0; }
.command-action, .inbox-error button { padding: 0 var(--co-space-3); font-size: 12px; font-weight: 700; }
.icon-action:hover, .command-action:hover, .inbox-error button:hover { border-color: var(--co-border-strong); color: var(--co-text-primary); background: var(--co-bg-hover); }
button:disabled { cursor: not-allowed; opacity: 0.55; }
.stream-state { display: flex; align-items: center; gap: var(--co-space-2); margin: 0; color: var(--co-text-muted); font-size: 11px; }
.stream-state span { width: 7px; height: 7px; border-radius: 50%; background: var(--co-status-neutral-fg); }
.stream-state.is-connected span { background: var(--co-status-success-fg); }
.stream-state.is-reconnecting span { background: var(--co-status-warning-fg); }
.inbox-error { padding: var(--co-space-4); border: 1px solid var(--co-status-critical-border); border-radius: var(--co-radius-panel); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.inbox-error p { margin: 0 0 var(--co-space-3); overflow-wrap: anywhere; }
.inbox-empty { display: grid; min-height: 180px; place-content: center; gap: var(--co-space-2); padding: var(--co-space-6); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); text-align: center; }
.inbox-empty strong { color: var(--co-text-primary); }
.notification-list { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.notification-list li { display: grid; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.notification-list li.is-unread { border-left: 3px solid var(--co-action-primary); background: var(--co-bg-subtle); }
.notification-list p { margin: 0; color: var(--co-text-primary); overflow-wrap: anywhere; }
.notification-meta { min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 11px; }
.notification-meta time { margin-left: auto; font-variant-numeric: tabular-nums; }
.severity { padding: 2px 6px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); font-family: var(--co-font-mono); font-weight: 800; }
.severity-p1, .severity-p2 { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.severity-p3 { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.source { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.notification-footer { justify-content: space-between; gap: var(--co-space-3); }
.notification-footer a { color: var(--co-action-primary); font-size: 12px; font-weight: 750; }
.notification-footer a:hover { color: var(--co-action-hover); text-decoration: underline; }
.unavailable-link { color: var(--co-text-muted); font-size: 12px; }
@media (max-width: 420px) {
  .inbox-heading { align-items: flex-start; }
  .command-action { width: 38px; overflow: hidden; padding: 0; color: transparent; gap: 0; }
  .command-action svg { color: var(--co-text-secondary); }
  .notification-meta time { width: 100%; margin-left: 0; }
}
</style>
