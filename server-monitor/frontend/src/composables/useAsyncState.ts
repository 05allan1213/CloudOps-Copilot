import { ref, computed } from "vue";

export function useAsyncState() {
  const loading = ref(false);
  const error = ref("");

  const state = computed<"loading" | "error" | "default">(() => {
    if (loading.value) return "loading";
    if (error.value) return "error";
    return "default";
  });

  function reset() {
    loading.value = false;
    error.value = "";
  }

  return { loading, error, state, reset };
}