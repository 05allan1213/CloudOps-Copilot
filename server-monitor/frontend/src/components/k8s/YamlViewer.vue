<script setup lang="ts">
import { ref, watch } from "vue";

import { fetchK8sResourceYAML } from "../../api/k8s";

const props = defineProps<{
  visible: boolean;
  kind: string;
  namespace: string;
  name: string;
}>();

const emit = defineEmits<{
  "update:visible": [value: boolean];
}>();

const loading = ref(false);
const error = ref("");
const yamlContent = ref("");

watch(
  () => props.visible,
  async (val) => {
    if (!val) return;
    if (!props.kind || !props.namespace || !props.name) return;

    loading.value = true;
    error.value = "";
    yamlContent.value = "";

    try {
      yamlContent.value = await fetchK8sResourceYAML(
        props.kind,
        props.namespace,
        props.name,
      );
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "Failed to load YAML";
    } finally {
      loading.value = false;
    }
  },
);

function handleClose() {
  emit("update:visible", false);
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    :title="`${kind}/${namespace}/${name}`"
    width="720px"
    top="6vh"
    destroy-on-close
    @close="handleClose"
  >
    <div
      v-loading="loading"
      class="yaml-viewer"
    >
      <el-alert
        v-if="error"
        :title="error"
        type="error"
        show-icon
        :closable="false"
        style="margin-bottom: 12px"
      />
      <pre
        v-if="yamlContent && !error"
        class="yaml-content"
      >{{ yamlContent }}</pre>
      <el-empty
        v-if="!loading && !error && !yamlContent"
        description="无 YAML 内容"
      />
    </div>
    <template #footer>
      <el-button @click="handleClose">
        关闭
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.yaml-viewer {
  min-height: 120px;
  max-height: 70vh;
  overflow-y: auto;
}

.yaml-content {
  margin: 0;
  padding: 12px;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-wrap: break-word;
  color: var(--el-text-color-primary);
  background-color: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
}
</style>
