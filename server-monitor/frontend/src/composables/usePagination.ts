import { computed, ref } from "vue";

export function usePagination(defaultPageSize = 20) {
  const page = ref(1);
  const pageSize = ref(defaultPageSize);
  const total = ref(0);

  const totalPages = computed(() =>
    Math.max(1, Math.ceil(total.value / pageSize.value)),
  );

  function goToPage(p: number) {
    page.value = Math.min(Math.max(p, 1), totalPages.value);
  }

  function resetPage() {
    page.value = 1;
  }

  return {
    page,
    pageSize,
    total,
    totalPages,
    goToPage,
    resetPage,
  };
}
