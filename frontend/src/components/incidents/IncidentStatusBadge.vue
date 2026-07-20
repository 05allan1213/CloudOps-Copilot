<script setup lang="ts">
import { computed } from "vue";
import { incidentStatusLabel, statusTone } from "../../models/incidents";

const props = defineProps<{ status: string; label?: string }>();
const text = computed(() => props.label ?? incidentStatusLabel(props.status));
</script>

<template>
  <el-tag
    :type="statusTone(status)"
    effect="plain"
  >
    {{ text }}
  </el-tag>
</template>
