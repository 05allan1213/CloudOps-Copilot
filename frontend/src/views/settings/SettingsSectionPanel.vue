<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import UAlert from "@nuxt/ui/components/Alert.vue";
import UBadge from "@nuxt/ui/components/Badge.vue";
import UButton from "@nuxt/ui/components/Button.vue";
import UFormField from "@nuxt/ui/components/FormField.vue";
import UTable from "@nuxt/ui/components/Table.vue";
import UTextarea from "@nuxt/ui/components/Textarea.vue";
import { computed } from "vue";

import type { ConfigurationRevision, ConfigurationValidation } from "../../api/platform";
import {
  isSettingsSectionDirty,
  sectionChangedInRevision,
  settingsSectionChanges,
  settingsSectionFingerprint,
  type SettingsApplyItemResult,
  type SettingsApplyOutcome,
  type SettingsSectionDraft,
} from "./settingsDraft";

const props = defineProps<{
  anchor: string;
  title: string;
  eyebrow: string;
  description: string;
  formId: string;
  section: SettingsSectionDraft;
  activeRevision: ConfigurationRevision;
  validation: ConfigurationValidation | null;
  validatedFingerprint: string;
  validating: boolean;
  applying: boolean;
  error: string;
  outcome: SettingsApplyOutcome | null;
}>();

const emit = defineEmits<{
  apply: [];
  reset: [];
  rebase: [preserveLocalValue: boolean];
  refreshOutcome: [];
  updateSummary: [value: string];
}>();

const dirty = computed(() => isSettingsSectionDirty(props.section));
const changes = computed(() => settingsSectionChanges(props.section));
const hasValueChanges = computed(() => changes.value.length > 0);
const revisionDrift = computed(() => (
  props.activeRevision.id !== props.section.baseRevisionID
  || props.activeRevision.hash !== props.section.baseRevisionHash
));
const concurrentSectionChange = computed(() => (
  revisionDrift.value && sectionChangedInRevision(props.section, props.activeRevision)
));
const validationExpired = computed(() => {
  if (!props.validation) return false;
  const expiresAt = new Date(props.validation.expires_at).getTime();
  return Number.isFinite(expiresAt) && expiresAt <= Date.now();
});
const validationStale = computed(() => Boolean(
  props.validation
  && (props.validatedFingerprint !== settingsSectionFingerprint(props.section)
    || validationExpired.value
    || revisionDrift.value),
));
const canApply = computed(() => Boolean(
  hasValueChanges.value
  && props.validation?.valid
  && !validationStale.value
  && !props.validating
  && !props.applying,
));

const outcomeColumns: TableColumn<SettingsApplyItemResult>[] = [
  { accessorKey: "label", header: "目标" },
  { accessorKey: "state", header: "状态" },
  { accessorKey: "detail", header: "当前观测" },
  { accessorKey: "observedAt", header: "Observed at (UTC)" },
];

function outcomeColor(state: SettingsApplyOutcome["state"]): "success" | "warning" | "error" | "neutral" {
  if (state === "succeeded") return "success";
  if (state === "failed") return "error";
  if (state === "partial" || state === "unknown") return "warning";
  return "neutral";
}
</script>

<template>
  <section
    :id="anchor"
    class="settings-section"
    :aria-labelledby="`${anchor}-heading`"
    tabindex="-1"
  >
    <header class="settings-section-heading">
      <div>
        <p class="settings-eyebrow">
          {{ eyebrow }}
        </p>
        <h2 :id="`${anchor}-heading`">
          {{ title }}
        </h2>
        <p>{{ description }}</p>
      </div>
      <div class="settings-section-identity">
        <UBadge
          :color="dirty ? 'warning' : 'neutral'"
          variant="soft"
          :icon="dirty ? 'i-lucide-pencil-line' : 'i-lucide-circle-check'"
          :label="dirty ? '本地草稿有变更' : '与基线一致'"
        />
        <code>base #{{ section.baseRevisionNumber }}</code>
      </div>
    </header>

    <UAlert
      v-if="revisionDrift"
      color="warning"
      variant="soft"
      icon="i-lucide-git-compare-arrows"
      title="活动 Revision 已变化"
      :description="concurrentSectionChange
        ? `本区基于 Revision #${section.baseRevisionNumber}；活动 Revision #${activeRevision.number} 也修改了本区。必须显式放弃或 rebase，系统不会覆盖。`
        : `本区基于 Revision #${section.baseRevisionNumber}；活动 Revision 已是 #${activeRevision.number}。本区内容未被并发修改，仍需显式 rebase 后重新验证。`"
    >
      <template #actions>
        <div class="settings-inline-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            label="放弃本区修改并刷新"
            @click="emit('rebase', false)"
          />
          <UButton
            color="warning"
            variant="outline"
            icon="i-lucide-git-branch"
            label="保留本区修改并 rebase"
            @click="emit('rebase', true)"
          />
        </div>
      </template>
    </UAlert>

    <slot />

    <UFormField
      label="Revision 摘要"
      :name="`${section.key}.summary`"
      required
      help="只描述本区变更；发布后进入 Configuration Revision 历史。"
      :data-field="`${section.key}.summary`"
    >
      <UTextarea
        :model-value="section.summary"
        :rows="2"
        :maxlength="255"
        autoresize
        class="settings-control"
        placeholder="说明变更原因和预期影响"
        @update:model-value="emit('updateSummary', String($event))"
      />
    </UFormField>

    <div
      class="settings-change-summary"
      aria-live="polite"
    >
      <div>
        <strong>变更摘要</strong>
        <span>{{ changes.length }} 项</span>
      </div>
      <p v-if="changes.length === 0">
        当前 section 与其基线 Revision 一致。
      </p>
      <ul v-else>
        <li
          v-for="change in changes"
          :key="change"
        >
          {{ change }}
        </li>
      </ul>
    </div>

    <UAlert
      v-if="error"
      color="error"
      variant="soft"
      icon="i-lucide-circle-x"
      title="本区操作失败"
      :description="error"
    />

    <div
      v-if="validation"
      class="settings-validation"
      aria-live="polite"
    >
      <UAlert
        :color="validation.valid && !validationStale ? 'success' : validation.valid ? 'warning' : 'error'"
        variant="soft"
        :icon="validation.valid && !validationStale ? 'i-lucide-shield-check' : 'i-lucide-shield-alert'"
        :title="validation.valid ? (validationStale ? 'Validation 已失效' : 'Validation 通过') : 'Validation 未通过'"
        :description="validationStale
          ? '本地草稿、有效期或活动 Revision 已变化；必须重新验证。'
          : `draft hash ${validation.draft_hash} · expires ${validation.expires_at}`"
      />
      <ul
        v-if="validation.errors.length"
        class="settings-error-list"
      >
        <li
          v-for="item in validation.errors"
          :key="`${item.field}-${item.code}`"
        >
          <code>{{ item.code }}</code> {{ item.field }}: {{ item.message }}
        </li>
      </ul>
    </div>

    <div
      v-if="outcome"
      class="settings-outcome"
      aria-live="polite"
    >
      <div class="settings-outcome-heading">
        <div>
          <strong>{{ outcome.title }}</strong>
          <p>{{ outcome.description }}</p>
        </div>
        <UBadge
          :color="outcomeColor(outcome.state)"
          variant="soft"
          :label="outcome.state"
        />
      </div>
      <div class="settings-table-scroll">
        <UTable
          :data="outcome.items"
          :columns="outcomeColumns"
          empty="尚无逐项观测"
          class="settings-table"
        />
      </div>
      <UButton
        v-if="outcome.state !== 'succeeded'"
        color="neutral"
        variant="outline"
        icon="i-lucide-refresh-cw"
        label="刷新当前 Revision 观测"
        @click="emit('refreshOutcome')"
      />
      <p
        v-if="outcome.state !== 'succeeded'"
        class="settings-truth-note"
      >
        仅重新读取状态，不重放 apply，也不把 accepted 或 partial 描述为原子成功。
      </p>
    </div>

    <footer class="settings-section-actions">
      <span>{{ validationStale ? "Validation stale" : validation?.valid ? "Validation current" : "等待显式验证" }}</span>
      <UButton
        color="neutral"
        variant="ghost"
        icon="i-lucide-rotate-ccw"
        label="重置本区"
        :disabled="!dirty || validating || applying"
        @click="emit('reset')"
      />
      <UButton
        type="submit"
        :form="formId"
        color="neutral"
        variant="outline"
        icon="i-lucide-shield-check"
        label="验证本区草稿"
        :loading="validating"
        :disabled="!hasValueChanges || revisionDrift || applying"
      />
      <UButton
        color="warning"
        icon="i-lucide-upload-cloud"
        label="审阅并应用"
        :loading="applying"
        :disabled="!canApply"
        @click="emit('apply')"
      />
    </footer>
  </section>
</template>

<style scoped>
.settings-section { display: grid; min-width: 0; gap: var(--co-space-4); scroll-margin-top: 76px; padding-block: var(--co-space-5); border-top: 1px solid var(--co-border-default); outline: none; }
.settings-section:focus-visible { box-shadow: inset 3px 0 0 var(--co-focus-ring); }
.settings-section-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); }
.settings-section-heading h2 { margin: 0; font-size: 18px; letter-spacing: 0; }
.settings-section-heading p:not(.settings-eyebrow) { max-width: 72ch; margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 12px; }
.settings-eyebrow { margin: 0 0 var(--co-space-1); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.settings-section-identity { display: flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-2); }
.settings-section-identity code { color: var(--co-text-muted); font-size: 10px; }
.settings-inline-actions { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.settings-control { width: 100%; }
.settings-change-summary { display: grid; gap: var(--co-space-2); padding-block: var(--co-space-3); border-block: 1px solid var(--co-border-default); background: var(--co-bg-subtle); }
.settings-change-summary > div { display: flex; justify-content: space-between; gap: var(--co-space-3); padding-inline: var(--co-space-3); }
.settings-change-summary span, .settings-change-summary p, .settings-truth-note { color: var(--co-text-muted); font-size: 11px; }
.settings-change-summary p { margin: 0; padding-inline: var(--co-space-3); }
.settings-change-summary ul, .settings-error-list { display: grid; gap: var(--co-space-1); margin: 0; padding: 0 var(--co-space-3) 0 calc(var(--co-space-3) + 18px); color: var(--co-text-secondary); font-size: 11px; }
.settings-validation, .settings-outcome { display: grid; min-width: 0; gap: var(--co-space-3); }
.settings-error-list { padding-block: var(--co-space-3); border-bottom: 1px solid var(--co-status-critical-border); color: var(--co-status-critical-fg); overflow-wrap: anywhere; }
.settings-outcome { padding-block: var(--co-space-3); border-block: 1px solid var(--co-border-default); }
.settings-outcome-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.settings-outcome-heading p { margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 11px; }
.settings-table-scroll { min-width: 0; overflow-x: auto; }
.settings-table { min-width: 720px; }
.settings-truth-note { margin: calc(-1 * var(--co-space-2)) 0 0; }
.settings-section-actions { position: sticky; bottom: 0; z-index: var(--co-z-sticky); display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); padding: var(--co-space-2); border-block: 1px solid var(--co-border-default); background: color-mix(in srgb, var(--co-bg-canvas) 96%, transparent); backdrop-filter: blur(8px); }
.settings-section-actions > span { margin-right: auto; color: var(--co-text-muted); font-size: 10px; }
@media (max-width: 720px) {
  .settings-section-heading { flex-direction: column; }
  .settings-section-identity { width: 100%; justify-content: space-between; }
  .settings-section-actions { align-items: stretch; flex-direction: column; }
  .settings-section-actions > span { margin-right: 0; }
  .settings-section-actions :deep(button) { width: 100%; }
}
</style>
