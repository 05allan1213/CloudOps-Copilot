<template>
  <div class="not-found-page">
    <div class="not-found-content">
      <h1>404</h1>
      <p>页面未找到</p>
      <button type="button" @click="goHome">返回首页</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from "vue-router";

const router = useRouter();

function goHome() {
  router.push("/");
}
</script>

<style scoped>
.not-found-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}

.not-found-content {
  text-align: center;
}

.not-found-content h1 {
  font-size: 4rem;
  margin: 0;
  color: var(--text-muted);
}

.not-found-content p {
  font-size: 1.1rem;
  color: var(--text-secondary);
  margin: 0.5rem 0 1.5rem;
}

.not-found-content button {
  background: var(--primary);
  color: #fff;
  border: none;
  padding: 0.5rem 1.5rem;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.9rem;
}

.not-found-content button:hover {
  opacity: 0.9;
}
</style>
