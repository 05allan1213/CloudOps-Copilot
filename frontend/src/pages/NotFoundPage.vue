<script setup lang="ts">
import { useRouter } from "vue-router";

const router = useRouter();

function goHome() {
  router.push("/incidents");
}
</script>

<template>
  <div class="not-found-page">
    <el-result
      icon="warning"
      title="404"
      sub-title="The requested page could not be found."
    >
      <template #extra>
        <el-button
          type="primary"
          @click="goHome"
        >
          Return to Incidents
        </el-button>
      </template>
    </el-result>
  </div>
</template>

<style scoped>
.not-found-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}
</style>
