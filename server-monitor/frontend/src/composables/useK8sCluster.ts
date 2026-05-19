import { ref, onMounted } from "vue";
import { fetchK8sClusters } from "../api/k8s";

const clusters = ref<string[]>(["default"]);
const currentCluster = ref("default");
const loaded = ref(false);

export function useK8sCluster() {
  async function loadClusters() {
    if (loaded.value) return;
    try {
      clusters.value = await fetchK8sClusters();
      loaded.value = true;
    } catch {
      clusters.value = ["default"];
    }
  }

  function setCluster(name: string) {
    if (clusters.value.includes(name)) {
      currentCluster.value = name;
    }
  }

  onMounted(() => {
    loadClusters();
  });

  return {
    clusters,
    currentCluster,
    setCluster,
    loadClusters,
  };
}
