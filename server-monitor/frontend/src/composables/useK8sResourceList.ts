import { ref, type Ref } from "vue";
import { useAsyncState } from "./useAsyncState";

interface UseK8sResourceListOptions<T> {
  fetchFn: (params: { namespace?: string; search?: string; limit: number }) => Promise<{ items: T[]; total: number }>;
  initialNamespace?: string;
  pageSize?: number;
}

export function useK8sResourceList<T>(options: UseK8sResourceListOptions<T>) {
  const { loading, error, state } = useAsyncState();

  const items = ref<T[]>([]) as Ref<T[]>;
  const total = ref(0);
  const namespace = ref(options.initialNamespace ?? "");
  const searchText = ref("");
  const page = ref(1);
  const pageSize = options.pageSize ?? 10;

  async function loadResources() {
    loading.value = true;
    error.value = "";
    try {
      const result = await options.fetchFn({
        namespace: namespace.value || undefined,
        search: searchText.value || undefined,
        limit: pageSize,
      });
      items.value = result.items;
      total.value = result.total;
    } catch (err) {
      error.value = err instanceof Error ? err.message : "加载失败";
    } finally {
      loading.value = false;
    }
  }

  function applyFilters() {
    page.value = 1;
    loadResources();
  }

  function resetFilters() {
    namespace.value = "";
    searchText.value = "";
    page.value = 1;
    loadResources();
  }

  function handlePageChange(p: number) {
    page.value = p;
    loadResources();
  }

  return {
    items,
    total,
    loading,
    error,
    state,
    namespace,
    searchText,
    page,
    pageSize,
    loadResources,
    applyFilters,
    resetFilters,
    handlePageChange,
  };
}