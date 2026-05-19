<script setup lang="ts">
import { ref } from "vue";

import type { K8sPVSummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

defineProps<{
  pvs: K8sPVSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("persistentvolume");
const yamlNamespace = ref("default");
const yamlName = ref("");

function viewYaml(name: string) {
  yamlKind.value = "persistentvolume";
  yamlNamespace.value = "default";
  yamlName.value = name;
  yamlVisible.value = true;
}

function pvStatusType(status: string): "" | "success" | "warning" | "danger" | "info" {
  switch (status) {
    case "Bound": return "success";
    case "Available": return "";
    case "Released": return "warning";
    case "Failed": return "danger";
    default: return "info";
  }
}
</script>

<template>
  <el-table
    :data="pvs"
    stripe
    highlight-current-row
    style="width: 100%"
  >
    <el-table-column
      prop="name"
      label="名称"
      min-width="200"
      show-overflow-tooltip
    />
    <el-table-column
      prop="capacity"
      label="容量"
      width="100"
      align="center"
    />
    <el-table-column
      label="Access Modes"
      min-width="140"
      show-overflow-tooltip
    >
      <template #default="{ row }">
        {{ row.access_modes?.join(", ") || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="状态"
      width="110"
      align="center"
    >
      <template #default="{ row }">
        <el-tag
          size="small"
          :type="pvStatusType(row.status)"
        >
          {{ row.status }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column
      prop="claim_ref"
      label="Claim"
      min-width="180"
      show-overflow-tooltip
    />
    <el-table-column
      prop="storage_class"
      label="StorageClass"
      min-width="130"
      show-overflow-tooltip
    />
    <el-table-column
      label="Age"
      width="100"
      align="center"
    >
      <template #default="{ row }">
        {{ row.age || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        <el-button
          type="primary"
          link
          size="small"
          @click="viewYaml(row.name)"
        >
          YAML
        </el-button>
      </template>
    </el-table-column>
  </el-table>
  <YamlViewer
    v-model:visible="yamlVisible"
    :kind="yamlKind"
    :namespace="yamlNamespace"
    :name="yamlName"
  />
</template>
