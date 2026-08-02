<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from "vue";

import type { KubernetesResource, TopologySnapshot } from "../../api/infrastructure";
import { currentAtlasSemanticTheme } from "../../theme/atlasTheme";
import { createAtlasFrameLifecycle } from "./atlasLifecycle";

const props = withDefaults(defineProps<{
  snapshot: TopologySnapshot;
  selectedId?: string;
}>(), {
  selectedId: "",
});
const emit = defineEmits<{
  select: [resource: KubernetesResource];
  unavailable: [reason: string];
}>();

let host: HTMLDivElement | null = null;
let disposeScene: (() => void) | undefined;
let updateSelection: ((animate?: boolean) => void) | undefined;
let setSceneVisible: ((visible: boolean) => void) | undefined;
let viewportObserver: IntersectionObserver | undefined;
let nearViewport = false;
let buildToken = 0;

function setHost(value: unknown) {
  host = value instanceof HTMLDivElement ? value : null;
}

async function buildScene() {
  const token = ++buildToken;
  disposeScene?.();
  disposeScene = undefined;
  updateSelection = undefined;
  setSceneVisible = undefined;
  if (!host || !props.snapshot.nodes.length || !nearViewport) return;

  let cleanupPartialScene: (() => void) | undefined;
  try {
    const [THREE, controlsModule] = await Promise.all([
      import("three"),
      import("three/examples/jsm/controls/OrbitControls.js"),
    ]);
    if (token !== buildToken || !host) return;

    const sceneHost = host;
    const { OrbitControls } = controlsModule;
    const scene = new THREE.Scene();
    const camera = new THREE.OrthographicCamera(-12, 12, 8, -8, 0.1, 300);
    const renderer = new THREE.WebGLRenderer({
      antialias: true,
      alpha: false,
      powerPreference: "high-performance",
      preserveDrawingBuffer: false,
    });
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, window.innerWidth <= 1024 ? 1.5 : 2));
    renderer.domElement.dataset.atlasCanvas = "true";
    renderer.domElement.dataset.nodeCount = String(props.snapshot.nodes.length);
    renderer.domElement.dataset.renderState = "initializing";
    renderer.domElement.setAttribute("aria-hidden", "true");
    renderer.domElement.style.display = "block";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.touchAction = "none";
    renderer.domElement.style.userSelect = "none";
    sceneHost.replaceChildren(renderer.domElement);

    const ambientLight = new THREE.AmbientLight(0xffffff, 1.2);
    const keyLight = new THREE.DirectionalLight(0xffffff, 2);
    keyLight.position.set(14, 24, 18);
    scene.add(ambientLight, keyLight);

    const layerHeight: Record<KubernetesResource["layer"], number> = {
      namespace: 0,
      service: 2.2,
      workload: 4.4,
      pod: 6.6,
      node: -2.2,
      gateway: 8.8,
    };
    const resources = [...props.snapshot.nodes].sort((left, right) => left.id.localeCompare(right.id));
    const namespaces = [...new Set(resources.map((item) => item.namespace || "cluster"))].sort();
    const positions = new Map<string, InstanceType<typeof THREE.Vector3>>();
    const meshes: InstanceType<typeof THREE.Mesh>[] = [];
    const meshRecords: Array<{
      resource: KubernetesResource;
      mesh: InstanceType<typeof THREE.Mesh>;
      material: InstanceType<typeof THREE.MeshStandardMaterial>;
    }> = [];
    const geometries = new Set<InstanceType<typeof THREE.BufferGeometry>>();
    const materials = new Set<InstanceType<typeof THREE.Material>>();

    for (const namespace of namespaces) {
      const group = resources.filter((item) => (item.namespace || "cluster") === namespace);
      group.forEach((resource, index) => {
        const namespaceIndex = namespaces.indexOf(namespace);
        const x = (index % 6 - Math.min(2.5, (group.length - 1) / 2)) * 2.8;
        const z = (namespaceIndex - (namespaces.length - 1) / 2) * 9 + Math.floor(index / 6) * 2.6;
        const position = new THREE.Vector3(x, layerHeight[resource.layer], z);
        positions.set(resource.id, position);
        const geometry = new THREE.BoxGeometry(
          resource.layer === "namespace" ? 2.2 : 1.45,
          resource.layer === "namespace" ? 0.45 : 0.82,
          resource.layer === "namespace" ? 2.2 : 1.45,
        );
        const material = new THREE.MeshStandardMaterial({ roughness: 0.68, metalness: 0.08 });
        const mesh = new THREE.Mesh(geometry, material);
        mesh.position.copy(position);
        mesh.userData.resourceId = resource.id;
        scene.add(mesh);
        meshes.push(mesh);
        meshRecords.push({ resource, mesh, material });
        geometries.add(geometry);
        materials.add(material);
      });
    }

    const edgeMaterial = new THREE.LineBasicMaterial({ transparent: true, opacity: 0.68 });
    materials.add(edgeMaterial);
    for (const edge of props.snapshot.edges) {
      const source = positions.get(edge.source_id);
      const target = positions.get(edge.target_id);
      if (!source || !target) continue;
      const geometry = new THREE.BufferGeometry().setFromPoints([source, target]);
      geometries.add(geometry);
      const line = new THREE.Line(geometry, edgeMaterial);
      line.userData.relationship = edge.relation;
      scene.add(line);
    }

    const bounds = new THREE.Box3().setFromObject(scene);
    const center = bounds.getCenter(new THREE.Vector3());
    const size = bounds.getSize(new THREE.Vector3());
    camera.position.set(center.x + 24, center.y + 25, center.z + 30);
    camera.lookAt(center);
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.copy(center);
    controls.enableDamping = false;
    controls.enablePan = true;
    controls.minZoom = 0.55;
    controls.maxZoom = 3.5;
    controls.maxPolarAngle = Math.PI * 0.48;

    const frames = createAtlasFrameLifecycle(() => {
      renderer.render(scene, camera);
      renderer.domElement.dataset.renderState = "ready";
    });
    controls.addEventListener("change", frames.request);
    setSceneVisible = (visible) => {
      frames.setVisible(visible && !document.hidden);
      renderer.domElement.dataset.renderState = visible && !document.hidden ? "ready" : "paused";
    };

    let selectionFrame = 0;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    updateSelection = (animate = true) => {
      if (selectionFrame) window.cancelAnimationFrame(selectionFrame);
      selectionFrame = 0;
      const starts = meshRecords.map(({ mesh, material }) => ({
        mesh,
        material,
        scale: mesh.scale.x,
        emissive: material.emissiveIntensity,
      }));
      const apply = (progress: number) => {
        starts.forEach(({ mesh, material, scale, emissive }, index) => {
          const selected = meshRecords[index].resource.id === props.selectedId;
          const nextScale = selected ? 1.18 : 1;
          const nextEmissive = selected ? 0.72 : 0.18;
          mesh.scale.setScalar(scale + (nextScale - scale) * progress);
          material.emissiveIntensity = emissive + (nextEmissive - emissive) * progress;
        });
        renderer.domElement.dataset.selectedId = props.selectedId;
        frames.request();
      };
      if (!animate || reducedMotion.matches || document.hidden) {
        apply(1);
        return;
      }
      const startedAt = performance.now();
      const step = (now: number) => {
        const progress = Math.min(1, (now - startedAt) / 300);
        apply(1 - ((1 - progress) ** 3));
        if (progress < 1) selectionFrame = window.requestAnimationFrame(step);
        else selectionFrame = 0;
      };
      selectionFrame = window.requestAnimationFrame(step);
    };

    const applyTheme = () => {
      const theme = currentAtlasSemanticTheme();
      scene.background = new THREE.Color(theme.background);
      renderer.setClearColor(theme.background, 1);
      edgeMaterial.color.set(theme.edge);
      ambientLight.color.set(theme.light);
      keyLight.color.set(theme.light);
      for (const { resource, material } of meshRecords) {
        material.color.set(theme.layer[resource.layer]);
        material.emissive.set(theme.health[resource.health.state]);
      }
      frames.request();
    };
    applyTheme();
    updateSelection(false);

    const resize = () => {
      const width = Math.max(1, sceneHost.clientWidth);
      const height = Math.max(1, sceneHost.clientHeight);
      const aspect = width / height;
      const viewHeight = Math.max(18, size.z * 1.35, size.y * 1.8);
      camera.left = (-viewHeight * aspect) / 2;
      camera.right = (viewHeight * aspect) / 2;
      camera.top = viewHeight / 2;
      camera.bottom = -viewHeight / 2;
      camera.updateProjectionMatrix();
      renderer.setSize(width, height, false);
      renderer.domElement.dataset.viewport = `${width}x${height}`;
      frames.request();
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(sceneHost);

    const raycaster = new THREE.Raycaster();
    const pointer = new THREE.Vector2();
    let pointerDown = { x: 0, y: 0 };
    const intersections = (event: PointerEvent) => {
      const rect = renderer.domElement.getBoundingClientRect();
      if (!rect.width || !rect.height) return [];
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      raycaster.setFromCamera(pointer, camera);
      return raycaster.intersectObjects(meshes, false);
    };
    const pointerDownHandler = (event: PointerEvent) => {
      pointerDown = { x: event.clientX, y: event.clientY };
    };
    const pointerMoveHandler = (event: PointerEvent) => {
      renderer.domElement.style.cursor = intersections(event).length ? "pointer" : "grab";
    };
    const pointerUpHandler = (event: PointerEvent) => {
      if (Math.hypot(event.clientX - pointerDown.x, event.clientY - pointerDown.y) > 5) return;
      const hit = intersections(event)[0]?.object.userData.resourceId as string | undefined;
      const resource = hit ? props.snapshot.nodes.find((item) => item.id === hit) : undefined;
      if (resource) emit("select", resource);
    };
    renderer.domElement.addEventListener("pointerdown", pointerDownHandler);
    renderer.domElement.addEventListener("pointermove", pointerMoveHandler);
    renderer.domElement.addEventListener("pointerup", pointerUpHandler);

    const visibilityHandler = () => {
      frames.setVisible(nearViewport && !document.hidden);
      renderer.domElement.dataset.renderState = document.hidden ? "paused" : "resuming";
      if (!document.hidden) updateSelection?.(false);
    };
    const contextLostHandler = (event: Event) => {
      event.preventDefault();
      renderer.domElement.dataset.renderState = "context-lost";
      emit("unavailable", "WebGL context 已丢失，已切换到结构化资源视图。");
    };
    const contextRestoredHandler = () => void buildScene();
    document.addEventListener("visibilitychange", visibilityHandler);
    renderer.domElement.addEventListener("webglcontextlost", contextLostHandler);
    renderer.domElement.addEventListener("webglcontextrestored", contextRestoredHandler);

    const themeObserver = new MutationObserver(applyTheme);
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });

    resize();
    cleanupPartialScene = () => {
      if (selectionFrame) window.cancelAnimationFrame(selectionFrame);
      themeObserver.disconnect();
      document.removeEventListener("visibilitychange", visibilityHandler);
      resizeObserver.disconnect();
      renderer.domElement.removeEventListener("pointerdown", pointerDownHandler);
      renderer.domElement.removeEventListener("pointermove", pointerMoveHandler);
      renderer.domElement.removeEventListener("pointerup", pointerUpHandler);
      renderer.domElement.removeEventListener("webglcontextlost", contextLostHandler);
      renderer.domElement.removeEventListener("webglcontextrestored", contextRestoredHandler);
      controls.removeEventListener("change", frames.request);
      frames.dispose();
      controls.dispose();
      for (const geometry of geometries) geometry.dispose();
      for (const material of materials) material.dispose();
      renderer.dispose();
      renderer.domElement.remove();
      sceneHost.dataset.atlasDisposed = "true";
      setSceneVisible = undefined;
    };
    disposeScene = cleanupPartialScene;
  } catch (error) {
    cleanupPartialScene?.();
    emit("unavailable", error instanceof Error ? error.message : "WebGL 初始化失败");
  }
}

onMounted(() => {
  if (!host || typeof IntersectionObserver === "undefined") {
    nearViewport = true;
    void buildScene();
    return;
  }
  viewportObserver = new IntersectionObserver((entries) => {
    const next = entries.some((entry) => entry.isIntersecting);
    if (next === nearViewport) return;
    nearViewport = next;
    host?.setAttribute("data-atlas-viewport", next ? "active" : "deferred");
    if (next && !disposeScene) void buildScene();
    else setSceneVisible?.(next);
  }, { rootMargin: "240px 0px", threshold: 0.01 });
  viewportObserver.observe(host);
});
watch(() => [props.snapshot.content_hash, props.snapshot.nodes, props.snapshot.edges], () => {
  if (nearViewport) void buildScene();
}, { deep: false });
watch(() => props.selectedId, () => updateSelection?.());
onBeforeUnmount(() => {
  buildToken += 1;
  viewportObserver?.disconnect();
  viewportObserver = undefined;
  disposeScene?.();
  disposeScene = undefined;
  updateSelection = undefined;
});
</script>

<template>
  <div
    :ref="setHost"
    class="operations-atlas"
    data-testid="operations-atlas"
  />
</template>

<style scoped>
.operations-atlas {
  position: absolute;
  inset: 0;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  background: var(--co-bg-canvas);
}
</style>
