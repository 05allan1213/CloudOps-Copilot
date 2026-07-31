<script setup lang="ts">
import { ref } from "vue";

import type { QueryAuthorization, QueryDefinition } from "../../api/monitoring";

defineProps<{
  definitions: QueryDefinition[];
  authorizations: QueryAuthorization[];
  busyID: string;
}>();

const emit = defineEmits<{
  loadDefinition: [definition: QueryDefinition];
  authorizeDefinition: [definition: QueryDefinition];
  revokeAuthorization: [authorization: QueryAuthorization];
}>();

const tab = ref<"definitions" | "authorizations">("definitions");

function authorizationState(item: QueryAuthorization): string {
  if (item.revoked_at) return "已撤销";
  if (item.consumed_execution_id) return "已使用";
  return "有效";
}
</script>

<template>
  <section
    class="monitoring-assets"
    aria-labelledby="monitoring-assets-title"
  >
    <header>
      <div>
        <span>Definition &amp; Authorization</span>
        <h2 id="monitoring-assets-title">
          查询资产
        </h2>
      </div>
      <div
        class="monitoring-assets__tabs"
        role="tablist"
        aria-label="查询资产视图"
      >
        <UButton
          role="tab"
          :aria-selected="tab === 'definitions'"
          :color="tab === 'definitions' ? 'primary' : 'neutral'"
          :variant="tab === 'definitions' ? 'soft' : 'ghost'"
          label="已保存"
          @click="tab = 'definitions'"
        />
        <UButton
          role="tab"
          :aria-selected="tab === 'authorizations'"
          :color="tab === 'authorizations' ? 'primary' : 'neutral'"
          :variant="tab === 'authorizations' ? 'soft' : 'ghost'"
          label="Agent 授权"
          @click="tab = 'authorizations'"
        />
      </div>
    </header>

    <div
      v-if="tab === 'definitions'"
      class="monitoring-assets__list"
      role="tabpanel"
    >
      <WorkspaceState
        v-if="!definitions.length"
        kind="empty"
        title="暂无 Query Definition"
        description="成功执行查询后可保存为受控定义。"
      />
      <article
        v-for="definition in definitions"
        :key="definition.id"
        class="monitoring-assets__row"
      >
        <div>
          <strong>{{ definition.title }}</strong>
          <span>{{ definition.resource.name }} · revision {{ definition.revision }} · {{ definition.mode }}</span>
          <code :title="definition.query">{{ definition.query }}</code>
        </div>
        <div class="monitoring-assets__actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-undo-2"
            label="载入"
            @click="emit('loadDefinition', definition)"
          />
          <UButton
            color="primary"
            variant="soft"
            icon="i-lucide-shield-check"
            label="授权 Agent"
            :loading="busyID === definition.id"
            @click="emit('authorizeDefinition', definition)"
          />
        </div>
      </article>
    </div>

    <div
      v-else
      class="monitoring-assets__list"
      role="tabpanel"
    >
      <WorkspaceState
        v-if="!authorizations.length"
        kind="empty"
        title="暂无 Agent Query Authorization"
        description="精确执行或 Query Definition 尚未授权给 Agent。"
      />
      <article
        v-for="authorization in authorizations"
        :key="authorization.id"
        class="monitoring-assets__row"
      >
        <div>
          <strong>{{ authorization.mode === "run_once" ? "一次性精确查询" : "Query Definition 授权" }}</strong>
          <span>{{ authorization.resource.name }} · {{ authorizationState(authorization) }} · {{ authorization.query_mode }}</span>
          <code :title="authorization.query_hash">{{ authorization.query_hash }}</code>
        </div>
        <UButton
          color="error"
          variant="outline"
          icon="i-lucide-ban"
          label="撤销"
          :disabled="Boolean(authorization.revoked_at)"
          :loading="busyID === authorization.id"
          @click="emit('revokeAuthorization', authorization)"
        />
      </article>
    </div>
  </section>
</template>

<style scoped>
.monitoring-assets { min-width: 0; margin-top: var(--co-space-6); border-top: 1px solid var(--co-border-default); }
.monitoring-assets > header { display: flex; min-height: 56px; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.monitoring-assets > header > div:first-child { display: grid; gap: 1px; }
.monitoring-assets h2 { margin: 0; font-size: 16px; }
.monitoring-assets > header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.monitoring-assets__tabs,
.monitoring-assets__actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--co-space-1); }
.monitoring-assets__list { border-top: 1px solid var(--co-border-subtle); }
.monitoring-assets__row { display: grid; grid-template-columns: minmax(0, 1fr) auto; min-width: 0; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.monitoring-assets__row > div:first-child { display: grid; min-width: 0; gap: 3px; }
.monitoring-assets__row strong { font-size: 12px; }
.monitoring-assets__row span { color: var(--co-text-secondary); font-size: 11px; }
.monitoring-assets__row code { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1024px) {
  .monitoring-assets__row { grid-template-columns: minmax(0, 1fr); }
}
</style>
