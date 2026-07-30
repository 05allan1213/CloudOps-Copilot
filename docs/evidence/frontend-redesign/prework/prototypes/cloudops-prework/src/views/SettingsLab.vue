<script setup lang="ts">
import type { FormError, FormSubmitEvent } from "@nuxt/ui";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { onBeforeRouteLeave, useRouter } from "vue-router";

interface SettingsDraft {
  revision: number;
  summary: string;
  prometheusUrl: string;
  timeoutMs: number;
  maxResults: number;
  browserNotifications: boolean;
}

const router = useRouter();
const active = reactive<SettingsDraft>({
  revision: 13,
  summary: "Exact-SHA Provider validation",
  prometheusUrl: "http://prometheus.cloudops-system.svc:9090",
  timeoutMs: 5000,
  maxResults: 200,
  browserNotifications: false,
});
const draft = reactive<SettingsDraft>({ ...active });
const validating = ref(false);
const applying = ref(false);
const validationPassed = ref(false);
const partialResult = ref(false);
const retryingProvider = ref(false);
const retryPassed = ref(false);
const revisionConflict = ref(false);
const leaveModalOpen = ref(false);
const pendingRoute = ref("");
const section = ref("providers");

const sectionItems = [
  { label: "Operational Scope", value: "scope", icon: "i-lucide-scan-search" },
  { label: "Provider", value: "providers", icon: "i-lucide-plug-zap" },
  { label: "查询与保留", value: "limits", icon: "i-lucide-gauge" },
  { label: "Revision", value: "revision", icon: "i-lucide-git-compare-arrows" },
];
const timeoutItems = [
  { label: "3 秒", value: 3000 },
  { label: "5 秒", value: 5000 },
  { label: "10 秒", value: 10000 },
];

const dirty = computed(() => JSON.stringify(draft) !== JSON.stringify(active));
const changes = computed(() => {
  const items: string[] = [];
  if (draft.summary !== active.summary) items.push(`摘要：${active.summary} → ${draft.summary}`);
  if (draft.prometheusUrl !== active.prometheusUrl) items.push("Prometheus endpoint 已修改");
  if (draft.timeoutMs !== active.timeoutMs) items.push(`Timeout：${active.timeoutMs} → ${draft.timeoutMs}`);
  if (draft.maxResults !== active.maxResults) items.push(`Max results：${active.maxResults} → ${draft.maxResults}`);
  if (draft.browserNotifications !== active.browserNotifications) items.push("浏览器通知开关已修改");
  return items;
});

async function validate(state: Partial<SettingsDraft>): Promise<FormError[]> {
  validating.value = true;
  await new Promise((resolve) => window.setTimeout(resolve, 180));
  const errors: FormError[] = [];
  if (!state.summary?.trim()) errors.push({ name: "summary", message: "Revision 摘要不能为空" });
  if (!state.prometheusUrl?.startsWith("http://") && !state.prometheusUrl?.startsWith("https://")) errors.push({ name: "prometheusUrl", message: "必须使用 http 或 https 协议" });
  if (state.prometheusUrl?.includes("javascript:")) errors.push({ name: "prometheusUrl", message: "危险协议已拒绝" });
  if (!state.maxResults || state.maxResults < 10 || state.maxResults > 1000) errors.push({ name: "maxResults", message: "范围必须为 10–1000" });
  validating.value = false;
  validationPassed.value = errors.length === 0;
  return errors;
}

async function applyDraft(_event: FormSubmitEvent<SettingsDraft>) {
  if (revisionConflict.value) return;
  applying.value = true;
  await new Promise((resolve) => window.setTimeout(resolve, 350));
  Object.assign(active, draft, { revision: active.revision + 1 });
  draft.revision = active.revision;
  applying.value = false;
  partialResult.value = true;
  retryPassed.value = false;
}

function restoreDraft() {
  Object.assign(draft, active);
  validationPassed.value = false;
  revisionConflict.value = false;
  partialResult.value = false;
  retryPassed.value = false;
}

async function retryProvider() {
  retryingProvider.value = true;
  await new Promise((resolve) => window.setTimeout(resolve, 220));
  retryingProvider.value = false;
  partialResult.value = false;
  retryPassed.value = true;
}

function reloadConcurrentRevision() {
  active.revision += 1;
  active.summary = "Concurrent Owner revision";
  restoreDraft();
}

function requestNavigation(path: string) {
  if (!dirty.value) {
    void router.push(path);
    return;
  }
  pendingRoute.value = path;
  leaveModalOpen.value = true;
}

function discardAndLeave() {
  const target = pendingRoute.value || "/incidents";
  pendingRoute.value = "";
  leaveModalOpen.value = false;
  restoreDraft();
  void router.push(target);
}

onBeforeRouteLeave((to) => {
  if (!dirty.value || pendingRoute.value === to.fullPath) return true;
  pendingRoute.value = to.fullPath;
  leaveModalOpen.value = true;
  return false;
});

function preventDirtyUnload(event: BeforeUnloadEvent) {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = "";
}

watch(dirty, (value) => {
  if (value) window.addEventListener("beforeunload", preventDirtyUnload);
  else window.removeEventListener("beforeunload", preventDirtyUnload);
}, { immediate: true });
onBeforeUnmount(() => window.removeEventListener("beforeunload", preventDirtyUnload));
</script>

<template>
  <section class="workspace settings-lab" aria-labelledby="settings-lab-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="settings-lab-title" tabindex="-1">设置与 Revision</h1>
        <p>分区 Draft、显式校验、变更摘要、逐项结果与并发 Revision，不伪造后端事务。</p>
      </div>
      <div class="workspace-actions">
        <UBadge :color="dirty ? 'warning' : 'success'" :icon="dirty ? 'i-lucide-pencil-line' : 'i-lucide-circle-check'" :label="dirty ? '存在未保存草稿' : '与活动 Revision 一致'" />
        <UButton color="neutral" variant="outline" icon="i-lucide-rotate-ccw" label="恢复" :disabled="!dirty" @click="restoreDraft" />
      </div>
    </header>

    <div class="settings-layout">
      <aside class="settings-nav" aria-label="设置分区">
        <UButton
          v-for="item in sectionItems"
          :key="item.value"
          :color="section === item.value ? 'primary' : 'neutral'"
          :variant="section === item.value ? 'soft' : 'ghost'"
          :icon="item.icon"
          :label="item.label"
          block
          @click="section = item.value"
        />
      </aside>

      <section class="settings-form-band">
        <UForm :state="draft" :validate="validate" :validate-on="['blur', 'change']" class="settings-form" data-testid="settings-form" @submit="applyDraft">
          <div class="section-heading">
            <div><h2>Configuration Revision #{{ active.revision }}</h2><p class="section-copy">精确 hash：43c5bfa0a22c9765396f0d4a84f6c07cfe04e0bd7dede16b8da1bb7b4efe6ab8</p></div>
            <UBadge color="success" variant="subtle" icon="i-lucide-check-check" label="Worker 已观察" />
          </div>

          <div class="form-grid">
            <UFormField label="Revision 摘要" name="summary" required help="进入审计历史，不可用模糊文本。">
              <UInput v-model="draft.summary" icon="i-lucide-file-pen-line" data-testid="revision-summary" />
            </UFormField>
            <UFormField label="Prometheus endpoint" name="prometheusUrl" required help="仅允许 http/https。">
              <UInput v-model="draft.prometheusUrl" icon="i-lucide-link" data-testid="prometheus-url" />
            </UFormField>
            <UFormField label="Provider timeout" name="timeoutMs">
              <USelect v-model="draft.timeoutMs" :items="timeoutItems" value-key="value" />
            </UFormField>
            <UFormField label="最大结果数" name="maxResults">
              <UInputNumber v-model="draft.maxResults" :min="10" :max="1000" :step="10" data-testid="max-results" />
            </UFormField>
            <UFormField label="浏览器通知" name="browserNotifications" help="只影响 Owner UI，不改变 Provider 事实。">
              <USwitch v-model="draft.browserNotifications" label="允许 P1/P2 系统通知" />
            </UFormField>
          </div>

          <div class="change-summary" aria-live="polite">
            <div class="section-heading"><h2>变更摘要</h2><span>{{ changes.length }} 项</span></div>
            <UEmpty v-if="changes.length === 0" icon="i-lucide-list-checks" title="没有草稿变化" description="当前表单与活动 Revision 一致。" />
            <ul v-else><li v-for="item in changes" :key="item"><UIcon name="i-lucide-arrow-right" aria-hidden="true" />{{ item }}</li></ul>
          </div>

          <UAlert v-if="revisionConflict" color="error" variant="soft" icon="i-lucide-git-compare-arrows" title="Concurrent revision" description="活动 Revision 已从 #13 变化；重新加载后再应用，不自动覆盖。" data-testid="revision-conflict">
            <template #actions><UButton color="error" variant="outline" icon="i-lucide-refresh-cw" label="重新加载活动 Revision" data-testid="reload-revision" @click="reloadConcurrentRevision" /></template>
          </UAlert>
          <UAlert v-if="partialResult" color="warning" variant="soft" icon="i-lucide-list-restart" title="部分结果" description="Revision 已 accepted；Prometheus observed，Elasticsearch 仍需 retry。未声明原子成功。" data-testid="partial-result">
            <template #actions><UButton color="warning" variant="outline" icon="i-lucide-refresh-cw" label="重试 Elasticsearch" :loading="retryingProvider" data-testid="retry-provider" @click="retryProvider" /></template>
          </UAlert>
          <UAlert v-if="retryPassed" color="success" variant="soft" icon="i-lucide-circle-check" title="重试已观察" description="Elasticsearch 当前 Revision 已 observed；Verification 仍由独立流程决定。" data-testid="retry-passed" />

          <div class="apply-bar">
            <div class="validation-state">
              <UIcon :name="validating ? 'i-lucide-loader-circle' : validationPassed ? 'i-lucide-circle-check' : 'i-lucide-shield-question'" :class="{ spinning: validating }" aria-hidden="true" />
              <span>{{ validating ? '正在异步校验…' : validationPassed ? '草稿校验通过' : '等待校验' }}</span>
            </div>
            <UButton color="neutral" variant="outline" icon="i-lucide-git-compare-arrows" label="模拟 Revision 冲突" @click="revisionConflict = true" />
            <UButton type="submit" color="primary" icon="i-lucide-upload-cloud" label="显式应用" :loading="applying" :disabled="!dirty || revisionConflict" data-testid="apply-settings" />
          </div>
        </UForm>
      </section>
    </div>

    <div class="settings-footer-actions">
      <UButton color="neutral" variant="ghost" icon="i-lucide-arrow-left" label="返回 Incident" @click="requestNavigation('/incidents')" />
    </div>

    <UModal :open="leaveModalOpen" title="离开并放弃当前 Draft？" description="后端没有被伪造成 Draft 存储；离开只会丢弃本地编辑。" :dismissible="false" :close="false" data-testid="settings-leave-modal">
      <template #body><UAlert color="warning" variant="soft" icon="i-lucide-triangle-alert" title="未保存设置" description="Scope、活动 Revision 和当前 Provider 状态不会被清空。" /></template>
      <template #footer>
        <div class="modal-actions">
          <UButton color="neutral" variant="outline" icon="i-lucide-pencil" label="继续编辑" data-testid="stay-settings" @click="leaveModalOpen = false" />
          <UButton color="error" icon="i-lucide-log-out" label="放弃并离开" data-testid="leave-settings" @click="discardAndLeave" />
        </div>
      </template>
    </UModal>
  </section>
</template>

<style scoped>
.settings-lab { max-width: 1500px; margin: 0 auto; }
.settings-layout { display: grid; min-width: 0; grid-template-columns: 190px minmax(0, 1fr); border: 1px solid var(--co-border); background: var(--co-surface); }
.settings-nav { display: flex; min-width: 0; flex-direction: column; gap: 4px; padding: var(--co-space-3); border-right: 1px solid var(--co-border); }
.settings-form-band { min-width: 0; }
.settings-form { display: grid; min-width: 0; gap: var(--co-space-5); padding: var(--co-space-5); }
.section-copy { max-width: 70ch; margin: 4px 0 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
.form-grid { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.change-summary { min-width: 0; padding: var(--co-space-4); border: 1px solid var(--co-border); border-radius: var(--co-radius-panel); background: var(--co-surface-muted); }
.change-summary ul { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.change-summary li { display: flex; min-width: 0; align-items: flex-start; gap: 6px; color: var(--co-text-secondary); font-size: 11px; }
.apply-bar { position: sticky; bottom: 0; z-index: var(--co-z-sticky); display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); padding: var(--co-space-3); border: 1px solid var(--co-border); background: var(--co-overlay); box-shadow: 0 -6px 18px rgb(16 24 32 / 6%); }
.validation-state { display: inline-flex; min-width: 0; align-items: center; gap: 6px; margin-right: auto; color: var(--co-text-secondary); font-size: 11px; }
.spinning { animation: spin 0.8s linear infinite; }
.settings-footer-actions { display: flex; justify-content: flex-start; padding: var(--co-space-3) 0; }
.modal-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) {
  .settings-layout { grid-template-columns: 1fr; }
  .settings-nav { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--co-border); }
  .form-grid { grid-template-columns: 1fr; }
}
</style>
