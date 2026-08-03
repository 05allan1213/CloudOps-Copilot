<script setup lang="ts">
import { computed, ref, watch } from "vue";

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  description: string;
  target: string;
  effect: string;
  authority: string;
  version: string;
  exactHash?: string;
  recovery: string;
  confirmLabel: string;
  reasonRequired?: boolean;
  pending?: boolean;
  severity?: "primary" | "warning" | "error";
}>(), {
  exactHash: "",
  reasonRequired: true,
  pending: false,
  severity: "warning",
});

const emit = defineEmits<{
  "update:open": [value: boolean];
  confirm: [reason: string];
}>();

const reason = ref("");
const attempted = ref(false);
const valid = computed(() => !props.reasonRequired || Boolean(reason.value.trim()));

watch(() => props.open, (open) => {
  if (open) {
    reason.value = "";
    attempted.value = false;
  }
});

function submit() {
  attempted.value = true;
  if (!valid.value || props.pending) return;
  emit("confirm", reason.value.trim());
}

function requestOpen(value: boolean) {
  emit("update:open", value);
}
</script>

<template>
  <UModal
    :open="open"
    :title="title"
    :description="description"
    :dismissible="!pending"
    :close="{ 'aria-label': '关闭确认窗口' }"
    @update:open="requestOpen"
  >
    <template #body>
      <div class="incident-command-confirmation">
        <UAlert
          :color="severity"
          variant="soft"
          icon="i-lucide-triangle-alert"
          title="精确命令确认"
          :description="effect"
        />
        <dl>
          <div><dt>Target</dt><dd>{{ target }}</dd></div>
          <div><dt>Authority</dt><dd>{{ authority }}</dd></div>
          <div><dt>Version</dt><dd>{{ version }}</dd></div>
          <div v-if="exactHash">
            <dt>Exact hash</dt><dd><code translate="no">{{ exactHash }}</code></dd>
          </div>
          <div><dt>恢复限制</dt><dd>{{ recovery }}</dd></div>
        </dl>
        <UFormField
          label="命令原因"
          :required="reasonRequired"
          :error="attempted && !valid ? '提交前请填写可审计原因。' : undefined"
          help="原因会随精确版本命令持久化；前端不是安全边界。"
        >
          <UTextarea
            v-model="reason"
            :rows="5"
            :maxlength="2048"
            autoresize
            placeholder="说明已核对的 Evidence、影响和恢复限制…"
          />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <div class="incident-command-actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="取消"
          :disabled="pending"
          @click="emit('update:open', false)"
        />
        <UButton
          :color="severity"
          icon="i-lucide-shield-check"
          :label="confirmLabel"
          :loading="pending"
          :disabled="!valid || pending"
          @click="submit"
        />
      </div>
    </template>
  </UModal>
</template>

<style scoped>
.incident-command-confirmation { display: grid; min-width: 0; gap: var(--co-space-4); }
.incident-command-confirmation dl { display: grid; margin: 0; gap: var(--co-space-1); }
.incident-command-confirmation dl div { display: grid; grid-template-columns: 120px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-default); }
.incident-command-confirmation dt { color: var(--co-text-muted); font-size: 11px; }
.incident-command-confirmation dd { min-width: 0; margin: 0; color: var(--co-text-primary); overflow-wrap: anywhere; }
.incident-command-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
</style>
