<script setup lang="ts">
import { computed, ref, useId } from "vue";
import { Copy as CopyDocument } from "lucide-vue-next";

const props = withDefaults(defineProps<{
  value: string;
  fileMode?: string;
  path?: string;
}>(), {
  fileMode: "",
  path: "",
});

const copyStatus = ref("");
const titleID = useId();
const lineCount = computed(() => props.value ? props.value.split("\n").length : 0);
const formattedLineCount = computed(() => new Intl.NumberFormat("en-US").format(lineCount.value));

async function copyDiff() {
  if (!props.value || !navigator.clipboard) {
    copyStatus.value = "Copy is unavailable in this browser.";
    return;
  }
  try {
    await navigator.clipboard.writeText(props.value);
    copyStatus.value = "Complete diff copied.";
  } catch {
    copyStatus.value = "The diff could not be copied.";
  }
}
</script>

<template>
  <section
    class="code-diff"
    :aria-labelledby="titleID"
  >
    <header>
      <div>
        <span>Complete Bounded Diff</span>
        <h4 :id="titleID">
          <code translate="no">{{ path || "Projected change" }}</code>
        </h4>
      </div>
      <div class="diff-meta">
        <span>{{ formattedLineCount }} line{{ lineCount === 1 ? "" : "s" }}</span>
        <code
          v-if="fileMode"
          translate="no"
        >{{ fileMode }}</code>
        <button
          type="button"
          aria-label="Copy complete bounded diff"
          title="Copy complete bounded diff"
          @click="copyDiff"
        >
          <CopyDocument
            :size="16"
            aria-hidden="true"
          />
        </button>
      </div>
    </header>

    <div
      class="diff-scroll"
      tabindex="0"
      aria-label="Complete bounded remediation diff; horizontally and vertically scrollable"
    >
      <pre><code>{{ value || "No bounded diff was projected." }}</code></pre>
    </div>
    <p
      class="copy-status"
      role="status"
      aria-live="polite"
    >
      {{ copyStatus }}
    </p>
  </section>
</template>

<style scoped>
.code-diff {
  display: grid;
  min-width: 0;
  gap: var(--co-space-3);
}

.code-diff > header,
.diff-meta {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.code-diff > header > div:first-child { min-width: 0; }
.code-diff > header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.code-diff h4 { min-width: 0; margin: 2px 0 0; font-size: 14px; overflow-wrap: anywhere; }
.code-diff h4 code { white-space: normal; }

.diff-meta { flex: 0 0 auto; justify-content: flex-end; }
.diff-meta > code { color: var(--co-text-muted); font-size: 11px; }
.diff-meta button { display: grid; width: 44px; height: 44px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.diff-meta button:hover { color: var(--co-action-primary); border-color: var(--co-action-primary); background: var(--co-bg-hover); }

.diff-scroll {
  max-width: 100%;
  max-height: 540px;
  overflow: auto;
  overscroll-behavior: contain;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  color: var(--co-text-primary);
  background: var(--co-bg-canvas);
}

pre {
  width: max-content;
  min-width: 100%;
  margin: 0;
  padding: var(--co-space-4);
  tab-size: 2;
  white-space: pre;
}

pre code { font-size: 12px; line-height: 1.55; }
.copy-status { min-height: 18px; margin: 0; color: var(--co-status-success-fg); font-size: 12px; }

@media (max-width: 640px) {
  .code-diff > header { align-items: flex-start; flex-direction: column; }
  .diff-meta { width: 100%; justify-content: space-between; }
  .diff-scroll { max-height: 420px; }
}
</style>
