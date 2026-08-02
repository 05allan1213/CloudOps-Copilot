<script setup lang="ts">
import { computed } from "vue";

import { renderAgentMarkdown } from "./agentMarkdown";

const props = defineProps<{ source: string }>();
const rendered = computed(() => renderAgentMarkdown(props.source));
</script>

<template>
  <!-- HTML is emitted only by the allowlisted Markdown renderer in agentMarkdown.ts. -->
  <!-- eslint-disable vue/no-v-html -->
  <div
    class="agent-markdown"
    v-html="rendered"
  />
  <!-- eslint-enable vue/no-v-html -->
</template>

<style scoped>
.agent-markdown {
  min-width: 0;
  color: var(--co-text-secondary);
  font-size: 13px;
  line-height: 1.72;
  overflow-wrap: anywhere;
}
.agent-markdown :deep(> :first-child) { margin-top: 0; }
.agent-markdown :deep(> :last-child) { margin-bottom: 0; }
.agent-markdown :deep(p) { margin: 10px 0; }
.agent-markdown :deep(h1),
.agent-markdown :deep(h2),
.agent-markdown :deep(h3),
.agent-markdown :deep(h4) { margin: 20px 0 8px; color: var(--co-text-primary); font-weight: 740; letter-spacing: 0; line-height: 1.35; }
.agent-markdown :deep(h1) { font-size: 19px; }
.agent-markdown :deep(h2) { padding-bottom: 6px; border-bottom: 1px solid var(--co-border-default); font-size: 16px; }
.agent-markdown :deep(h3) { font-size: 14px; }
.agent-markdown :deep(h4) { font-size: 13px; }
.agent-markdown :deep(ul),
.agent-markdown :deep(ol) { margin: 10px 0; padding-left: 22px; }
.agent-markdown :deep(li) { margin: 4px 0; padding-left: 2px; }
.agent-markdown :deep(li::marker) { color: var(--co-status-info-fg); }
.agent-markdown :deep(blockquote) { margin: 12px 0; padding: 8px 12px; border-left: 3px solid var(--co-border-strong); color: var(--co-text-muted); background: var(--co-bg-subtle); }
.agent-markdown :deep(a) { color: var(--co-status-info-fg); text-decoration: underline; text-decoration-thickness: 1px; text-underline-offset: 3px; }
.agent-markdown :deep(a:hover) { color: var(--co-text-primary); }
.agent-markdown :deep(code) { padding: 2px 5px; border: 1px solid var(--co-border-default); border-radius: 4px; color: var(--co-text-primary); background: var(--co-code-bg); font-family: var(--co-font-mono); font-size: .9em; }
.agent-markdown :deep(pre) { max-width: 100%; margin: 12px 0; padding: 12px 14px; overflow: auto; border: 1px solid var(--co-border-default); border-radius: 7px; background: var(--co-code-bg); }
.agent-markdown :deep(pre code) { display: block; min-width: max-content; padding: 0; border: 0; color: var(--co-text-secondary); background: transparent; line-height: 1.6; white-space: pre; }
.agent-markdown :deep(table) { display: block; width: 100%; max-width: 100%; margin: 12px 0; overflow-x: auto; border-spacing: 0; border-collapse: collapse; }
.agent-markdown :deep(th),
.agent-markdown :deep(td) { min-width: 110px; padding: 8px 10px; border: 1px solid var(--co-border-default); text-align: left; vertical-align: top; }
.agent-markdown :deep(th) { color: var(--co-text-primary); background: var(--co-bg-subtle); font-size: 11px; font-weight: 720; }
.agent-markdown :deep(td) { font-size: 12px; }
.agent-markdown :deep(hr) { margin: 18px 0; border: 0; border-top: 1px solid var(--co-border-default); }
.agent-markdown :deep(input[type="checkbox"]) { margin-right: 7px; accent-color: var(--co-action-primary); }
</style>
