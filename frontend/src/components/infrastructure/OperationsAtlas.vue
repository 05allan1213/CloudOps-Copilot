<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from "vue";

import type { KubernetesResource, TopologySnapshot } from "../../api/infrastructure";

const props = defineProps<{ snapshot: TopologySnapshot; selectedId?: string }>();
const emit = defineEmits<{
  select: [resource: KubernetesResource];
  unavailable: [reason: string];
}>();

let host: HTMLDivElement | null = null;
let disposeScene: (() => void) | undefined;
let buildToken = 0;

function setHost(value: unknown) {
  host = value instanceof HTMLDivElement ? value : null;
}

async function buildScene() {
  const token = ++buildToken;
  disposeScene?.();
  disposeScene = undefined;
  if (!host || !props.snapshot.nodes.length) return;
  try {
    const [THREE, controlsModule] = await Promise.all([
      import("three"),
      import("three/examples/jsm/controls/OrbitControls.js"),
    ]);
    if (token !== buildToken || !host) return;
    const { OrbitControls } = controlsModule;
    const scene = new THREE.Scene();
    scene.background = new THREE.Color(0x090c10);
    const camera = new THREE.OrthographicCamera(-12, 12, 8, -8, 0.1, 300);
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false, powerPreference: "high-performance" });
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, window.innerWidth < 768 ? 1.25 : 2));
    renderer.domElement.dataset.atlasCanvas = "true";
    renderer.domElement.dataset.nodeCount = String(props.snapshot.nodes.length);
    renderer.domElement.setAttribute("aria-hidden", "true");
    renderer.domElement.style.display = "block";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    host.appendChild(renderer.domElement);

    scene.add(new THREE.AmbientLight(0xffffff, 1.25));
    const keyLight = new THREE.DirectionalLight(0xffffff, 2.1);
    keyLight.position.set(14, 24, 18);
    scene.add(keyLight);

    const layerHeight: Record<KubernetesResource["layer"], number> = {
      namespace: 0,
      service: 2.2,
      workload: 4.4,
      pod: 6.6,
      node: -2.2,
      gateway: 8.8,
    };
    const layerColor: Record<KubernetesResource["layer"], number> = {
      namespace: 0x5a6472,
      service: 0x24c7d9,
      workload: 0x8b7cf6,
      pod: 0x52d273,
      node: 0xe7a441,
      gateway: 0xe460a8,
    };
    const healthColor: Record<KubernetesResource["health"]["state"], number> = {
      healthy: 0x4ade80,
      warning: 0xf59e0b,
      critical: 0xef4444,
      unknown: 0x94a3b8,
    };
    const resources = [...props.snapshot.nodes].sort((left, right) => left.id.localeCompare(right.id));
    const namespaces = [...new Set(resources.map((item) => item.namespace || "cluster"))].sort();
    const positions = new Map<string, InstanceType<typeof THREE.Vector3>>();
    const meshes: InstanceType<typeof THREE.Mesh>[] = [];
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
        const geometry = new THREE.BoxGeometry(resource.layer === "namespace" ? 2.2 : 1.45, resource.layer === "namespace" ? 0.45 : 0.82, resource.layer === "namespace" ? 2.2 : 1.45);
        const material = new THREE.MeshStandardMaterial({
          color: layerColor[resource.layer],
          emissive: healthColor[resource.health.state],
          emissiveIntensity: resource.id === props.selectedId ? 0.72 : 0.18,
          roughness: 0.68,
          metalness: 0.08,
        });
        const mesh = new THREE.Mesh(geometry, material);
        mesh.position.copy(position);
        mesh.userData.resourceId = resource.id;
        mesh.scale.setScalar(resource.id === props.selectedId ? 1.18 : 1);
        scene.add(mesh);
        meshes.push(mesh);
        geometries.add(geometry);
        materials.add(material);
      });
    }

    const edgeMaterial = new THREE.LineBasicMaterial({ color: 0x607080, transparent: true, opacity: 0.72 });
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

    let frame = 0;
    const render = () => {
      if (document.hidden || frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        renderer.render(scene, camera);
        renderer.domElement.dataset.renderState = "ready";
      });
    };
    controls.addEventListener("change", render);

    const resize = () => {
      if (!host) return;
      const width = Math.max(1, host.clientWidth);
      const height = Math.max(1, host.clientHeight);
      const aspect = width / height;
      const viewHeight = Math.max(18, size.z * 1.35, size.y * 1.8);
      camera.left = -viewHeight * aspect / 2;
      camera.right = viewHeight * aspect / 2;
      camera.top = viewHeight / 2;
      camera.bottom = -viewHeight / 2;
      camera.updateProjectionMatrix();
      renderer.setSize(width, height, false);
      render();
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(host);

    const raycaster = new THREE.Raycaster();
    const pointer = new THREE.Vector2();
    let pointerDown = { x: 0, y: 0 };
    const intersections = (event: PointerEvent) => {
      const rect = renderer.domElement.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      raycaster.setFromCamera(pointer, camera);
      return raycaster.intersectObjects(meshes, false);
    };
    const pointerDownHandler = (event: PointerEvent) => { pointerDown = { x: event.clientX, y: event.clientY }; };
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
    const visibilityHandler = () => { if (!document.hidden) render(); };
    document.addEventListener("visibilitychange", visibilityHandler);
    resize();

    disposeScene = () => {
      buildToken++;
      if (frame) window.cancelAnimationFrame(frame);
      document.removeEventListener("visibilitychange", visibilityHandler);
      resizeObserver.disconnect();
      renderer.domElement.removeEventListener("pointerdown", pointerDownHandler);
      renderer.domElement.removeEventListener("pointermove", pointerMoveHandler);
      renderer.domElement.removeEventListener("pointerup", pointerUpHandler);
      controls.removeEventListener("change", render);
      controls.dispose();
      for (const geometry of geometries) geometry.dispose();
      for (const material of materials) material.dispose();
      renderer.dispose();
      renderer.domElement.remove();
    };
  } catch (error) {
    emit("unavailable", error instanceof Error ? error.message : "WebGL 初始化失败");
  }
}

onMounted(buildScene);
watch(() => [props.snapshot.content_hash, props.snapshot.collected_at, props.selectedId], buildScene);
onBeforeUnmount(() => {
  buildToken++;
  disposeScene?.();
});
</script>

<template>
  <div :ref="setHost" class="operations-atlas" data-testid="operations-atlas" />
</template>

<style scoped>
.operations-atlas { position: absolute; inset: 0; min-width: 0; min-height: 0; overflow: hidden; background: #090c10; }
.operations-atlas :deep(canvas) { touch-action: none; user-select: none; -webkit-user-select: none; }
</style>
