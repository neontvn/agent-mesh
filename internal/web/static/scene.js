import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

const HEALTH_COLORS = {
    healthy:   0x5cd5fb,
    degraded:  0xf5b557,
    unhealthy: 0xff5577,
    unknown:   0x6b87b3,
};

let scene, camera, renderer, controls;
let container;
const agentGroups = new Map(); // agent_id -> THREE.Group
const activeArcs = [];         // [{ caller, callee, t, particle, arc, curve }]

// ===================== Init =====================

export function initScene(el) {
    container = el;

    scene = new THREE.Scene();
    scene.fog = new THREE.FogExp2(0x050a1a, 0.018);

    const w = container.clientWidth;
    const h = container.clientHeight;

    camera = new THREE.PerspectiveCamera(58, w / h, 0.1, 200);
    camera.position.set(0, 7, 18);
    camera.lookAt(0, 0, 0);

    renderer = new THREE.WebGLRenderer({ antialias: true });
    renderer.setPixelRatio(window.devicePixelRatio || 1);
    renderer.setSize(w, h);
    renderer.setClearColor(0x050a1a, 1);
    container.appendChild(renderer.domElement);

    controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.06;
    controls.maxPolarAngle = Math.PI / 2 - 0.05;
    controls.minDistance = 6;
    controls.maxDistance = 40;

    // Grid floor
    const grid = new THREE.GridHelper(80, 40, 0x1a3a6a, 0x0a1a3a);
    grid.position.y = -3;
    scene.add(grid);

    // Soft glow horizon (a large transparent plane)
    const horizonGeom = new THREE.PlaneGeometry(120, 30);
    const horizonMat = new THREE.MeshBasicMaterial({
        color: 0x1a4a8a,
        transparent: true,
        opacity: 0.12,
        side: THREE.DoubleSide,
    });
    const horizon = new THREE.Mesh(horizonGeom, horizonMat);
    horizon.position.set(0, 4, -25);
    scene.add(horizon);

    // Lighting
    scene.add(new THREE.AmbientLight(0x4a6a9a, 0.6));
    const dir = new THREE.DirectionalLight(0xa0d0ff, 0.5);
    dir.position.set(5, 12, 5);
    scene.add(dir);

    // A few faint background stars
    addStarfield();

    window.addEventListener('resize', onResize);
    animate();
}

function onResize() {
    const w = container.clientWidth;
    const h = container.clientHeight;
    camera.aspect = w / h;
    camera.updateProjectionMatrix();
    renderer.setSize(w, h);
}

function addStarfield() {
    const count = 220;
    const positions = new Float32Array(count * 3);
    for (let i = 0; i < count; i++) {
        positions[i * 3] = (Math.random() - 0.5) * 100;
        positions[i * 3 + 1] = Math.random() * 30 + 4;
        positions[i * 3 + 2] = -Math.random() * 60 - 10;
    }
    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    const mat = new THREE.PointsMaterial({
        color: 0xa4c4f0,
        size: 0.15,
        transparent: true,
        opacity: 0.6,
    });
    scene.add(new THREE.Points(geom, mat));
}

// ===================== Agents =====================

export function syncAgents(list) {
    const seen = new Set();
    for (const a of list) {
        seen.add(a.id);
        if (!agentGroups.has(a.id)) {
            const g = createAgentGroup(a);
            agentGroups.set(a.id, g);
            scene.add(g);
        } else {
            setHealth(a.id, a.health);
        }
    }
    // Remove agents that have disappeared
    for (const [id, group] of agentGroups) {
        if (!seen.has(id)) {
            scene.remove(group);
            disposeGroup(group);
            agentGroups.delete(id);
        }
    }
    layoutAgents();
}

function createAgentGroup(agent) {
    const group = new THREE.Group();
    const color = HEALTH_COLORS[agent.health] || HEALTH_COLORS.healthy;

    // Glowing sphere
    const sphereGeom = new THREE.SphereGeometry(0.7, 36, 36);
    const sphereMat = new THREE.MeshPhongMaterial({
        color: color,
        emissive: color,
        emissiveIntensity: 0.65,
        shininess: 90,
    });
    const sphere = new THREE.Mesh(sphereGeom, sphereMat);
    group.add(sphere);

    // Capability rings (one per declared capability, capped at 3)
    const rings = [];
    const ringCount = Math.max(1, Math.min(3, (agent.capabilities || []).length));
    for (let i = 0; i < ringCount; i++) {
        const inner = 1.25 + i * 0.18;
        const ringGeom = new THREE.RingGeometry(inner, inner + 0.03, 96);
        const ringMat = new THREE.MeshBasicMaterial({
            color: color,
            transparent: true,
            opacity: 0.55 - i * 0.15,
            side: THREE.DoubleSide,
        });
        const ring = new THREE.Mesh(ringGeom, ringMat);
        ring.rotation.x = Math.PI / 2;
        group.add(ring);
        rings.push(ring);
    }

    // Italic label sprite
    const label = makeLabel(agent.id);
    label.position.y = 1.55;
    group.add(label);

    group.userData = { sphere, rings, color, health: agent.health, agent };
    return group;
}

function disposeGroup(group) {
    group.traverse((obj) => {
        if (obj.geometry) obj.geometry.dispose();
        if (obj.material) {
            if (Array.isArray(obj.material)) obj.material.forEach((m) => m.dispose());
            else obj.material.dispose();
        }
    });
}

export function setHealth(agentId, health) {
    const group = agentGroups.get(agentId);
    if (!group) return;
    const color = HEALTH_COLORS[health] || HEALTH_COLORS.unknown;
    if (group.userData.color === color) return;
    group.userData.color = color;
    group.userData.health = health;
    group.userData.sphere.material.color.setHex(color);
    group.userData.sphere.material.emissive.setHex(color);
    for (const ring of group.userData.rings) {
        ring.material.color.setHex(color);
    }
}

function makeLabel(text) {
    const canvas = document.createElement('canvas');
    canvas.width = 512;
    canvas.height = 128;
    const ctx = canvas.getContext('2d');
    ctx.font = 'italic 38px "SF Pro Display", "Inter", sans-serif';
    ctx.fillStyle = 'rgba(214, 227, 244, 0.92)';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.shadowColor = 'rgba(92, 213, 251, 0.6)';
    ctx.shadowBlur = 12;
    ctx.fillText(text, 256, 64);
    const tex = new THREE.CanvasTexture(canvas);
    tex.minFilter = THREE.LinearFilter;
    const mat = new THREE.SpriteMaterial({ map: tex, transparent: true });
    const sprite = new THREE.Sprite(mat);
    sprite.scale.set(4, 1, 1);
    return sprite;
}

// Lay out agents in a loose constellation (slight randomness based on id hash).
function layoutAgents() {
    const ids = Array.from(agentGroups.keys()).sort();
    const n = ids.length;
    if (n === 0) return;
    const radius = Math.max(4, n * 0.9);
    ids.forEach((id, i) => {
        const angle = (i / n) * Math.PI * 2;
        const jitter = hashTo01(id);
        const r = radius * (0.85 + jitter * 0.3);
        const y = Math.sin(i * 0.7 + jitter) * 0.8;
        const group = agentGroups.get(id);
        group.position.set(Math.cos(angle) * r, y, Math.sin(angle) * r);
    });
}

function hashTo01(s) {
    let h = 2166136261;
    for (let i = 0; i < s.length; i++) {
        h ^= s.charCodeAt(i);
        h = (h * 16777619) >>> 0;
    }
    return (h % 1000) / 1000;
}

// ===================== Invoke animations =====================

export function animateInvoke(callerId, calleeId, ok = true) {
    const a = agentGroups.get(callerId);
    const b = agentGroups.get(calleeId);
    if (!a || !b) return;

    const start = a.position.clone();
    const end = b.position.clone();
    const mid = start.clone().add(end).multiplyScalar(0.5);
    const dist = start.distanceTo(end);
    mid.y += Math.min(4, dist * 0.45);
    const curve = new THREE.QuadraticBezierCurve3(start, mid, end);

    // Color the arc cyan for successful invokes, red for failed ones.
    const arcColor = ok ? 0x5cd5fb : 0xff5577;
    const particleColor = ok ? 0xffffff : 0xff99aa;

    const arcGeom = new THREE.TubeGeometry(curve, 48, 0.05, 8, false);
    const arcMat = new THREE.MeshBasicMaterial({
        color: arcColor,
        transparent: true,
        opacity: ok ? 0.55 : 0.7,
    });
    const arc = new THREE.Mesh(arcGeom, arcMat);
    scene.add(arc);

    // Bright particle that flies along the curve
    const partGeom = new THREE.SphereGeometry(0.13, 16, 16);
    const partMat = new THREE.MeshBasicMaterial({ color: particleColor, transparent: true, opacity: 1 });
    const particle = new THREE.Mesh(partGeom, partMat);
    scene.add(particle);

    activeArcs.push({
        caller: callerId,
        callee: calleeId,
        startedAt: performance.now(),
        durationMs: 1500,
        particle,
        arc,
        arcMat,
        curve,
        ok,
    });
}

// ===================== Animate loop =====================

function animate() {
    requestAnimationFrame(animate);
    controls.update();

    const t = performance.now();

    // Subtle pulsing on each agent sphere
    for (const [, group] of agentGroups) {
        const phase = group.userData.sphere.geometry.parameters.radius * 0.001 + t * 0.0015 + group.position.x * 0.1;
        group.userData.sphere.scale.setScalar(1 + Math.sin(phase) * 0.04);
        for (const ring of group.userData.rings) {
            ring.rotation.z += 0.001;
        }
    }

    // Animate active arcs and particles
    for (let i = activeArcs.length - 1; i >= 0; i--) {
        const inv = activeArcs[i];
        const elapsed = t - inv.startedAt;
        const progress = elapsed / inv.durationMs;

        if (progress >= 1) {
            scene.remove(inv.arc);
            scene.remove(inv.particle);
            inv.arc.geometry.dispose();
            inv.arc.material.dispose();
            inv.particle.geometry.dispose();
            inv.particle.material.dispose();
            activeArcs.splice(i, 1);
            continue;
        }

        const point = inv.curve.getPoint(progress);
        inv.particle.position.copy(point);
        inv.particle.material.opacity = 1 - progress * 0.6;
        inv.arcMat.opacity = Math.max(0, 0.55 * (1 - progress));
    }

    renderer.render(scene, camera);
}
