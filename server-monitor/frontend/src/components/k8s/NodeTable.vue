<script setup lang="ts">
import { useRouter } from "vue-router";

import type { K8sNodeWithHost } from "../../types/k8s";

import K8sStatusBadge from "./K8sStatusBadge.vue";

defineProps<{
  nodes: K8sNodeWithHost[];
}>();

const router = useRouter();

function handleRowClick(row: K8sNodeWithHost) {
  router.push(`/k8s/nodes/${encodeURIComponent(row.node.name)}`);
}
</script>

<template>
  <el-table
    :data="nodes"
    stripe
    highlight-current-row
    style="width: 100%"
    @row-click="handleRowClick"
  >
    <el-table-column prop="node.name" label="名称" min-width="180" show-overflow-tooltip />
    <el-table-column label="状态" width="100" align="center">
      <template #default="{ row }">
        <K8sStatusBadge :status="row.node.ready ? 'Ready' : 'NotReady'" type="node" />
      </template>
    </el-table-column>
    <el-table-column label="角色" min-width="120">
      <template #default="{ row }">
        {{ (row.node.roles ?? []).join(", ") || "-" }}
      </template>
    </el-table-column>
    <el-table-column prop="node.kubelet_version" label="Kubelet" min-width="120" show-overflow-tooltip />
    <el-table-column label="CPU" width="100">
      <template #default="{ row }">
        {{ row.node.capacity?.cpu || "-" }}
      </template>
    </el-table-column>
    <el-table-column label="内存" width="100">
      <template #default="{ row }">
        {{ row.node.capacity?.memory || "-" }}
      </template>
    </el-table-column>
    <el-table-column label="主机关联" width="100" align="center">
      <template #default="{ row }">
        <el-tag v-if="row.host_online" type="success" size="small">已关联</el-tag>
        <el-tag v-else type="info" size="small">未关联</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="80" align="center">
      <template #default="{ row }">
        <el-button
          type="primary"
          link
          size="small"
          @click.stop="router.push(`/k8s/nodes/${encodeURIComponent(row.node.name)}`)"
        >
          详情
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>
