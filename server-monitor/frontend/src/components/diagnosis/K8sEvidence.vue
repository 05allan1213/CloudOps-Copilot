<script setup lang="ts">
import { formatTime } from "../../utils/format";
import type { K8sEvidence as K8sEvidenceType } from "../../types";

defineProps<{
  k8sEvidence: K8sEvidenceType;
}>();

function formatServicePorts(ports?: Array<{ port: number; target_port?: string }>) {
  return (ports ?? []).map((p) => `${p.port}${p.target_port ? `:${p.target_port}` : ""}`).join(", ") || "-";
}
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <span class="card-title">K8s 证据</span>
    </template>

    <div
      v-if="!k8sEvidence?.enabled"
      class="empty-hint"
    >
      当前诊断未采集 K8s 证据。
    </div>

    <template v-else>
      <div class="k8s-head">
        <el-tag size="small">
          {{ k8sEvidence.namespace || "-" }}
        </el-tag>
        <strong>{{ k8sEvidence.target_kind || "-" }} / {{ k8sEvidence.target_name || "-" }}</strong>
        <small>{{ formatTime(k8sEvidence.collected_at) }}</small>
      </div>

      <el-collapse>
        <el-collapse-item
          v-if="k8sEvidence.deployments?.length"
          :name="'deployments'"
          :title="`Deployments (${k8sEvidence.deployments.length})`"
        >
          <el-table
            :data="k8sEvidence.deployments"
            stripe
            size="small"
            style="width: 100%"
          >
            <el-table-column
              label="Deployment"
              min-width="160"
            >
              <template #default="{ row }">
                {{ row.namespace }}/{{ row.name }}
              </template>
            </el-table-column>
            <el-table-column
              label="ready"
              width="90"
              align="center"
            >
              <template #default="{ row }">
                {{ row.ready_replicas }}/{{ row.replicas }}
              </template>
            </el-table-column>
            <el-table-column
              prop="updated_replicas"
              label="updated"
              width="80"
              align="center"
            />
            <el-table-column
              prop="available_replicas"
              label="available"
              width="90"
              align="center"
            />
          </el-table>
        </el-collapse-item>

        <el-collapse-item
          v-if="k8sEvidence.pods?.length"
          :name="'pods'"
          :title="`Pods (${k8sEvidence.pods.length})`"
        >
          <el-table
            :data="k8sEvidence.pods"
            stripe
            size="small"
            style="width: 100%"
          >
            <el-table-column
              label="Pod"
              min-width="160"
            >
              <template #default="{ row }">
                {{ row.namespace }}/{{ row.name }}
              </template>
            </el-table-column>
            <el-table-column
              prop="phase"
              label="phase"
              width="90"
              align="center"
            />
            <el-table-column
              label="ready"
              width="80"
              align="center"
            >
              <template #default="{ row }">
                {{ row.ready_containers }}/{{ row.total_containers }}
              </template>
            </el-table-column>
            <el-table-column
              prop="restart_count"
              label="restarts"
              width="90"
              align="center"
            />
          </el-table>
        </el-collapse-item>

        <el-collapse-item
          v-if="k8sEvidence.services?.length"
          :name="'services'"
          :title="`Services (${k8sEvidence.services.length})`"
        >
          <el-table
            :data="k8sEvidence.services"
            stripe
            size="small"
            style="width: 100%"
          >
            <el-table-column
              label="Service"
              min-width="160"
            >
              <template #default="{ row }">
                {{ row.namespace }}/{{ row.name }}
              </template>
            </el-table-column>
            <el-table-column
              prop="type"
              label="type"
              width="90"
            />
            <el-table-column
              prop="cluster_ip"
              label="cluster IP"
              width="120"
            />
            <el-table-column
              label="ports"
              min-width="120"
            >
              <template #default="{ row }">
                {{ formatServicePorts(row.ports) }}
              </template>
            </el-table-column>
          </el-table>
        </el-collapse-item>

        <el-collapse-item
          v-if="k8sEvidence.nodes?.length"
          :name="'nodes'"
          :title="`Nodes (${k8sEvidence.nodes.length})`"
        >
          <el-table
            :data="k8sEvidence.nodes"
            stripe
            size="small"
            style="width: 100%"
          >
            <el-table-column
              prop="name"
              label="Node"
              min-width="120"
            />
            <el-table-column
              label="ready"
              width="80"
              align="center"
            >
              <template #default="{ row }">
                <el-tag
                  :type="row.ready ? 'success' : 'danger'"
                  size="small"
                >
                  {{ row.ready ? "true" : "false" }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="kubelet_version"
              label="kubelet"
              width="120"
            />
            <el-table-column
              label="capacity"
              min-width="120"
            >
              <template #default="{ row }">
                {{ row.capacity?.cpu || "-" }} / {{ row.capacity?.memory || "-" }}
              </template>
            </el-table-column>
          </el-table>
        </el-collapse-item>

        <el-collapse-item
          v-if="k8sEvidence.events?.length"
          :name="'events'"
          :title="`Events (${k8sEvidence.events.length})`"
        >
          <el-table
            :data="k8sEvidence.events"
            stripe
            size="small"
            style="width: 100%"
          >
            <el-table-column
              prop="type"
              label="type"
              width="80"
            />
            <el-table-column
              prop="reason"
              label="reason"
              width="120"
            />
            <el-table-column
              prop="message"
              label="message"
              min-width="200"
              show-overflow-tooltip
            />
          </el-table>
        </el-collapse-item>

        <el-collapse-item
          v-for="log in k8sEvidence.logs"
          :key="`${log.namespace}-${log.pod_name}-${log.container}`"
          :name="`log-${log.namespace}-${log.pod_name}`"
          :title="`${log.namespace}/${log.pod_name}${log.container ? ` · ${log.container}` : ''} 日志`"
        >
          <pre class="log-content">{{ (log.lines || []).join("\n") }}</pre>
        </el-collapse-item>
      </el-collapse>

      <el-alert
        v-for="item in k8sEvidence.errors"
        :key="`${item.source}-${item.error}`"
        :title="`${item.source}：${item.error}`"
        type="error"
        show-icon
        :closable="false"
        style="margin-top: 8px"
      />
    </template>
  </el-card>
</template>

<style scoped>
.card-title {
  font-size: 14px;
  font-weight: 600;
}

.empty-hint {
  color: var(--el-text-color-placeholder);
  text-align: center;
  padding: 16px 0;
}

.k8s-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 12px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

.k8s-head strong {
  color: var(--el-text-color-secondary);
}

.log-content {
  max-height: 220px;
  overflow: auto;
  color: var(--el-text-color-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
  margin: 0;
}
</style>
