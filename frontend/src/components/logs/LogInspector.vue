<script setup lang="ts">
import { computed, ref } from "vue";

import type { LogEntry } from "../../api/telemetry";
import { logRawValue } from "../../models/telemetry";
import { safeExternalURL } from "../../models/workbench";
import WorkspaceInspector from "../workspace/WorkspaceInspector.vue";

const props = defineProps<{
  open: boolean;
  entry: LogEntry | null;
  targetID: string;
  trigger: HTMLElement | null;
  selected: boolean;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  toggleEvidence: [entryID: string];
  openTrace: [entry: LogEntry];
}>();

const copied = ref(false);
const providerLink = computed(() => {
  const link = props.entry?.links.find((item) => item.target === "external" && item.availability === "available");
  const href = safeExternalURL(link?.href);
  return link && href ? { label: link.label || "Elasticsearch", href } : null;
});

function exactUTC(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toISOString();
}

function fallbackCopy(value: string) {
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

async function copyRaw() {
  if (!props.entry) return;
  const value = logRawValue(props.entry.message);
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    fallbackCopy(value);
  }
  copied.value = true;
  window.setTimeout(() => { copied.value = false; }, 1200);
}
</script>

<template>
  <WorkspaceInspector
    :open="open"
    :title="entry ? `${entry.level?.toUpperCase() || 'INFO'} 日志` : '日志详情'"
    :description="entry ? `${entry.resource.kind} ${entry.resource.name}` : targetID"
    :target-state="entry ? 'ready' : 'invalid'"
    :target-description="entry ? '' : `当前 Query Execution 中不存在日志 ${targetID}。`"
    :trigger="trigger"
    @update:open="emit('update:open', $event)"
  >
    <template v-if="entry">
      <section
        class="log-inspector__raw"
        aria-labelledby="log-raw-heading"
      >
        <div>
          <h3 id="log-raw-heading">
            完整原文
          </h3>
          <UButton
            color="neutral"
            variant="ghost"
            :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
            :label="copied ? '已复制' : '复制原文'"
            @click="copyRaw"
          />
        </div>
        <pre><code>{{ entry.message }}</code></pre>
      </section>

      <section aria-labelledby="log-fields-heading">
        <h3 id="log-fields-heading">
          投影字段
        </h3>
        <dl class="log-inspector__fields">
          <div><dt>timestamp UTC</dt><dd>{{ exactUTC(entry.timestamp) }}</dd></div>
          <div><dt>service</dt><dd>{{ entry.service || "无" }}</dd></div>
          <div><dt>trace_id</dt><dd>{{ entry.trace_id || "无" }}</dd></div>
          <div><dt>span_id</dt><dd>{{ entry.span_id || "无" }}</dd></div>
          <div><dt>resource</dt><dd>{{ entry.resource.id }}</dd></div>
          <div
            v-for="(value, key) in entry.attributes"
            :key="key"
          >
            <dt>{{ key }}</dt><dd>{{ value }}</dd>
          </div>
        </dl>
      </section>
    </template>

    <template #recovery>
      <UButton
        color="neutral"
        variant="outline"
        icon="i-lucide-arrow-left"
        label="返回日志结果"
        @click="emit('update:open', false)"
      />
    </template>

    <template #footer>
      <UButton
        v-if="entry"
        color="neutral"
        :variant="selected ? 'soft' : 'outline'"
        :icon="selected ? 'i-lucide-check' : 'i-lucide-file-plus-2'"
        :label="selected ? '已选为 Evidence' : '选为 Evidence'"
        @click="emit('toggleEvidence', entry.id)"
      />
      <UButton
        v-if="entry?.trace_id"
        color="primary"
        variant="soft"
        icon="i-lucide-git-branch"
        label="打开 Trace"
        @click="emit('openTrace', entry)"
      />
      <UButton
        v-if="providerLink"
        color="neutral"
        variant="outline"
        icon="i-lucide-external-link"
        :label="providerLink.label"
        :to="providerLink.href"
        target="_blank"
        rel="noopener noreferrer"
        external
      />
    </template>
  </WorkspaceInspector>
</template>

<style scoped>
.log-inspector__raw,
.log-inspector__fields { min-width: 0; }
.log-inspector__raw > div { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.log-inspector__raw h3,
section > h3 { margin: 0 0 var(--co-space-2); font-size: 14px; }
.log-inspector__raw pre {
  max-height: 280px;
  margin: 0;
  overflow: auto;
  padding: var(--co-space-3);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  background: var(--co-code-bg);
  color: var(--co-code-text);
}
.log-inspector__raw code { font-family: var(--co-font-mono); font-size: 11px; white-space: pre; }
.log-inspector__fields { margin: 0; }
.log-inspector__fields div { display: grid; grid-template-columns: minmax(100px, 0.7fr) minmax(0, 1fr); gap: var(--co-space-2); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.log-inspector__fields dt,
.log-inspector__fields dd { min-width: 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
.log-inspector__fields dt { color: var(--co-text-muted); }
.log-inspector__fields dd { margin: 0; }
</style>
