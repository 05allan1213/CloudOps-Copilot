<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
	CircleMinus as RemoveFilled,
	CircleX as CircleCloseFilled,
	Info as InfoFilled,
	Lock,
	RefreshCw as Refresh,
	TriangleAlert as WarningFilled,
} from "lucide-vue-next";

import type { LoadState } from "../../types/incidents";

type StateBlockState = Exclude<LoadState, "ready"> | "disabled";

const props = withDefaults(defineProps<{
  state: StateBlockState;
  headingLevel?: 2 | 3;
  title?: string;
  message?: string;
  requestID?: string;
  traceID?: string;
  busy?: boolean;
  primaryActionLabel?: string;
  secondaryActionLabel?: string;
}>(), {
  headingLevel: 3,
  title: "",
  message: "",
  requestID: "",
  traceID: "",
  busy: false,
  primaryActionLabel: "",
  secondaryActionLabel: "",
});

defineEmits<{
  primaryAction: [];
  secondaryAction: [];
}>();

const definitions: Record<StateBlockState, { title: string; message: string; icon: Component; tone: string }> = {
  loading: { title: "正在加载", message: "正在获取最新服务端投影…", icon: markRaw(Refresh), tone: "neutral" },
  empty: { title: "没有 Incident", message: "当前筛选条件没有匹配的 Incident。", icon: markRaw(RemoveFilled), tone: "neutral" },
  error: { title: "无法加载 Incident", message: "请重试；若持续失败，请使用下方请求标识排查。", icon: markRaw(CircleCloseFilled), tone: "danger" },
  forbidden: { title: "请求被拒绝", message: "Local Owner 边界拒绝了此请求，请核对 Origin 与精确命令前置条件。", icon: markRaw(Lock), tone: "warning" },
  not_found: { title: "未找到投影", message: "请求的 Incident 投影不存在或已不可用。", icon: markRaw(InfoFilled), tone: "neutral" },
  unavailable: { title: "Incident 投影暂不可用", message: "API 当前无法提供此投影，请在依赖恢复后重试。", icon: markRaw(WarningFilled), tone: "warning" },
  disabled: { title: "操作不可用", message: "当前状态不允许此操作。", icon: markRaw(Lock), tone: "neutral" },
};

const definition = computed(() => definitions[props.state]);
const effectiveTitle = computed(() => props.title || definition.value.title);
const effectiveMessage = computed(() => props.message || definition.value.message);
const headingTag = computed(() => `h${props.headingLevel}`);
const role = computed(() => ["error", "forbidden", "unavailable"].includes(props.state) ? "alert" : "status");
</script>

<template>
  <section
    class="state-block"
    :class="`state-block--${definition.tone}`"
    :role="role"
    :aria-live="role === 'alert' ? 'assertive' : 'polite'"
  >
    <span
      class="state-icon"
      aria-hidden="true"
    >
      <component
        :is="definition.icon"
        :size="22"
      />
    </span>
    <div class="state-copy">
      <component :is="headingTag">
        {{ effectiveTitle }}
      </component>
      <p>{{ effectiveMessage }}</p>
      <dl
        v-if="requestID || traceID"
        class="request-identity"
      >
        <div v-if="requestID">
          <dt>Request ID</dt>
          <dd><code translate="no">{{ requestID }}</code></dd>
        </div>
        <div v-if="traceID">
          <dt>Trace ID</dt>
          <dd><code translate="no">{{ traceID }}</code></dd>
        </div>
      </dl>
      <div
        v-if="primaryActionLabel || secondaryActionLabel"
        class="state-actions"
      >
        <UButton
          v-if="primaryActionLabel"
          color="primary"
          icon="i-lucide-refresh-cw"
          :label="primaryActionLabel"
          :loading="busy"
          :disabled="busy"
          @click="$emit('primaryAction')"
        />
        <UButton
          v-if="secondaryActionLabel"
          color="neutral"
          variant="outline"
          :label="secondaryActionLabel"
          :disabled="busy"
          @click="$emit('secondaryAction')"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.state-block {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr);
  gap: var(--co-space-4);
  padding: var(--co-space-6);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.state-block--danger { border-color: var(--co-status-critical-border); }
.state-block--warning { border-color: var(--co-status-warning-border); }

.state-icon {
  display: grid;
  width: 40px;
  height: 40px;
  place-items: center;
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
}

.state-block--danger .state-icon { color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.state-block--warning .state-icon { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }

.state-copy,
.state-copy :is(h2, h3),
.state-copy p,
.request-identity {
  min-width: 0;
  margin: 0;
}

.state-copy :is(h2, h3) {
  color: var(--co-text-primary);
  font-size: 16px;
}

.state-copy p {
  margin-top: var(--co-space-1);
  color: var(--co-text-secondary);
}

.request-identity {
  display: grid;
  gap: var(--co-space-2);
  margin-top: var(--co-space-4);
}

.request-identity div {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: var(--co-space-2);
}

.request-identity dt {
  color: var(--co-text-muted);
  font-size: 12px;
}

.request-identity dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}

.state-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--co-space-2);
  margin-top: var(--co-space-4);
}

.state-actions :deep(.el-button) {
  min-height: 44px;
}

@media (max-width: 560px) {
  .state-block {
    grid-template-columns: minmax(0, 1fr);
    padding: var(--co-space-5);
  }
}
</style>
