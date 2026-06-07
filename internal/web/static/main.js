import { initScene, syncAgents, animateInvoke, setHealth } from './scene.js';

// ===================== State =====================

// agent_id -> { id, capabilities, health, lastSeen, endpoint }
const agents = new Map();

// Sliding window of recent invoke timestamps (for "Active calls/sec").
let recentInvokes = [];

// Sliding window of invoke timestamps for the timeline histogram.
let timelineInvokes = [];

// task_id -> { taskId, agent, capability, state, lastSeen }
const tasks = new Map();

// ===================== WebSocket =====================

let ws;
let reconnectDelay = 1000;

function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.onopen = () => {
        console.log('[mesh] connected');
        reconnectDelay = 1000;
    };

    ws.onclose = () => {
        console.log(`[mesh] disconnected, reconnecting in ${reconnectDelay}ms`);
        setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 10000);
    };

    ws.onmessage = (msg) => {
        try {
            const event = JSON.parse(msg.data);
            handleEvent(event);
        } catch (err) {
            console.error('bad event', err, msg.data);
        }
    };
}

function handleEvent(event) {
    const ts = new Date(event.timestamp);
    const d = event.data || {};

    switch (event.type) {
        case 'agent_registered': {
            const existing = agents.get(d.agent_id) || {};
            agents.set(d.agent_id, {
                id: d.agent_id,
                capabilities: d.capabilities || [],
                endpoint: d.endpoint || '',
                health: existing.health || 'healthy',
                lastSeen: ts,
            });
            break;
        }
        case 'agent_unregistered':
            agents.delete(d.agent_id);
            break;

        case 'agent_heartbeat': {
            // Auto-bootstrap from heartbeats so clients that connect after
            // the register event still pick up existing agents.
            let a = agents.get(d.agent_id);
            if (!a) {
                a = {
                    id: d.agent_id,
                    capabilities: d.capabilities || [],
                    endpoint: d.endpoint || '',
                    health: d.health || 'healthy',
                    lastSeen: ts,
                };
                agents.set(d.agent_id, a);
            } else {
                a.lastSeen = ts;
                if (d.health) a.health = d.health;
                if (d.capabilities && d.capabilities.length) a.capabilities = d.capabilities;
                if (d.endpoint) a.endpoint = d.endpoint;
            }
            break;
        }

        case 'agent_health_changed': {
            const a = agents.get(d.agent_id);
            if (a) {
                a.health = d.health || a.health;
                setHealth(d.agent_id, a.health);
            }
            break;
        }

        case 'invoke_completed': {
            recentInvokes.push(ts.getTime());
            timelineInvokes.push(ts.getTime());
            if (d.caller_id && d.callee_id) {
                // Auto-create spheres for callers/callees the UI hasn't
                // seen registered (e.g., the planner, which acts as a
                // mesh client without registering itself).
                ensureAgent(d.caller_id);
                ensureAgent(d.callee_id);
                syncAgents(Array.from(agents.values()));
                animateInvoke(d.caller_id, d.callee_id, d.ok !== false);
            }
            logEvent({
                time: ts,
                source: d.caller_id || '',
                dest: d.callee_id || '',
                method: d.method || '',
                capability: d.capability || '',
                bytes: Number(d.payload_bytes || 0),
                ok: d.ok !== false,
                latency: Number(d.duration_ms || 0),
            });
            break;
        }

        case 'task_updated': {
            const id = d.task_id;
            if (id) {
                const existing = tasks.get(id) || {};
                tasks.set(id, {
                    taskId: id,
                    agent: d.agent_id || existing.agent || '',
                    capability: d.capability || existing.capability || '',
                    state: d.state || existing.state || 'submitted',
                    lastSeen: ts,
                });
            }
            break;
        }

        default:
            break;
    }

    renderSidebar();
    renderTasks();
    syncAgents(Array.from(agents.values()));
    updateEmptyState();
}

// ===================== UI rendering =====================

function renderSidebar() {
    const list = document.getElementById('agent-list');
    const sorted = Array.from(agents.values()).sort((a, b) => a.id.localeCompare(b.id));

    list.innerHTML = '';
    for (const a of sorted) {
        const li = document.createElement('li');
        li.className = 'agent-item';
        const caps = a.capabilities.map((c) => `<span class="cap-pill">${escapeHTML(c)}</span>`).join('');
        li.innerHTML = `
            <div class="agent-row">
                <div class="agent-name">
                    <span class="status-dot ${a.health || 'unknown'}"></span>
                    ${escapeHTML(a.id)}
                </div>
                <span class="agent-time">${formatRelative(a.lastSeen)}</span>
            </div>
            <div class="cap-pills">${caps}</div>
        `;
        list.appendChild(li);
    }
}

// Render the most-recently-updated tasks with a state badge.
function renderTasks() {
    const list = document.getElementById('task-list');
    if (!list) return;
    const sorted = Array.from(tasks.values())
        .sort((a, b) => b.lastSeen - a.lastSeen)
        .slice(0, 12);

    list.innerHTML = '';
    for (const t of sorted) {
        const li = document.createElement('li');
        li.className = 'task-item';
        li.innerHTML = `
            <div class="task-row">
                <span class="task-cap">${escapeHTML(t.capability || '—')}</span>
                <span class="badge ${escapeHTML(t.state)}">${escapeHTML(t.state)}</span>
            </div>
            <div class="task-meta">
                <span class="task-agent">${escapeHTML(t.agent || '')}</span>
                <span class="task-time">${formatRelative(t.lastSeen)}</span>
            </div>
        `;
        list.appendChild(li);
    }
}

function formatRelative(date) {
    if (!date) return '—';
    const sec = Math.max(0, (Date.now() - date.getTime()) / 1000);
    if (sec < 60) return `${Math.floor(sec)}s ago`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
    return `${Math.floor(sec / 3600)}h ago`;
}

function escapeHTML(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

// ensureAgent ensures an entry exists in the agents map for the given id.
// Used when we learn about an agent through invoke events (caller side)
// rather than the register/heartbeat stream.
function ensureAgent(id) {
    if (!agents.has(id)) {
        agents.set(id, {
            id,
            capabilities: [],
            endpoint: '',
            health: 'unknown',
            lastSeen: new Date(),
        });
    }
}

function updateEmptyState() {
    const empty = document.getElementById('empty-state');
    if (!empty) return;
    empty.classList.toggle('hidden', agents.size > 0);
}

// "Active calls/sec" over the last 5 seconds.
function updateActiveCalls() {
    const cutoff = Date.now() - 5000;
    recentInvokes = recentInvokes.filter((t) => t > cutoff);
    const rate = recentInvokes.length / 5;
    document.getElementById('active-calls').textContent = rate.toFixed(1);
}

// ===================== Live event stream =====================

const eventLog = [];
let streamPaused = false;

// logEvent records one invoke and re-renders the stream unless paused.
function logEvent(rec) {
    eventLog.push(rec);
    if (eventLog.length > 500) eventLog.shift();
    if (!streamPaused) renderEventStream();
}

function renderEventStream() {
    const tbody = document.getElementById('es-rows');
    if (!tbody) return;
    const rows = eventLog.slice(-60).reverse();
    tbody.innerHTML = rows.map((r) => {
        const status = r.ok
            ? '<span class="es-ok">✓</span>'
            : '<span class="es-fail">✗</span>';
        const latClass = r.latency > 100 ? 'es-lat-slow' : 'es-lat';
        const payload = `${escapeHTML(r.capability || '—')} <span class="es-size">${formatBytes(r.bytes)}</span>`;
        return `<tr>
            <td class="es-time">${formatClock(r.time)}</td>
            <td class="es-node">${escapeHTML(r.source)}</td>
            <td class="es-arrow">→</td>
            <td class="es-node">${escapeHTML(r.dest)}</td>
            <td class="es-method">${escapeHTML(r.method || '—')}</td>
            <td>${payload}</td>
            <td>${status}</td>
            <td class="${latClass}">${r.latency}ms</td>
        </tr>`;
    }).join('');
}

function formatClock(d) {
    const p = (n, l = 2) => String(n).padStart(l, '0');
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

function formatBytes(n) {
    if (!n) return '0 B';
    if (n < 1024) return `${n} B`;
    return `${(n / 1024).toFixed(1)} KB`;
}

function exportEvents() {
    const header = ['time', 'source', 'destination', 'method', 'capability', 'payload_bytes', 'status', 'latency_ms'];
    const lines = [header.join(',')];
    for (const r of eventLog) {
        lines.push([
            r.time.toISOString(), r.source, r.dest, r.method, r.capability,
            r.bytes, r.ok ? 'ok' : 'fail', r.latency,
        ].join(','));
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'agentmesh-events.csv';
    a.click();
    URL.revokeObjectURL(url);
}

// Drag the handle at the top of the panel to resize it. The 3D viewport
// re-fits because scene.js listens for window resize.
function wireResize() {
    const handle = document.getElementById('es-resize');
    if (!handle) return;
    let dragging = false;

    const apply = (clientY) => {
        const footer = 64;
        let h = (window.innerHeight - footer) - clientY;
        h = Math.max(120, Math.min(h, Math.round(window.innerHeight * 0.7)));
        document.documentElement.style.setProperty('--es-height', `${h}px`);
        window.dispatchEvent(new Event('resize'));
    };

    handle.addEventListener('mousedown', (e) => {
        dragging = true;
        handle.classList.add('dragging');
        document.body.style.userSelect = 'none';
        e.preventDefault();
    });
    window.addEventListener('mousemove', (e) => {
        if (dragging) apply(e.clientY);
    });
    window.addEventListener('mouseup', () => {
        if (!dragging) return;
        dragging = false;
        handle.classList.remove('dragging');
        document.body.style.userSelect = '';
    });
}

function wireStreamControls() {
    const pauseBtn = document.getElementById('es-pause');
    const exportBtn = document.getElementById('es-export');
    const liveBadge = document.getElementById('es-live');
    if (pauseBtn) {
        pauseBtn.addEventListener('click', () => {
            streamPaused = !streamPaused;
            pauseBtn.textContent = streamPaused ? '▶ Resume' : '⏸ Pause';
            if (liveBadge) {
                liveBadge.textContent = streamPaused ? '● Paused' : '● Live';
                liveBadge.classList.toggle('paused', streamPaused);
            }
            if (!streamPaused) renderEventStream();
        });
    }
    if (exportBtn) exportBtn.addEventListener('click', exportEvents);
}

// ===================== Timeline canvas =====================

const timelineCanvas = document.getElementById('timeline');
const timelineCtx = timelineCanvas.getContext('2d');

function drawTimeline() {
    const dpr = window.devicePixelRatio || 1;
    const w = timelineCanvas.clientWidth * dpr;
    const h = timelineCanvas.clientHeight * dpr;
    timelineCanvas.width = w;
    timelineCanvas.height = h;

    timelineCtx.clearRect(0, 0, w, h);

    const now = Date.now();
    const windowMs = 60 * 1000;
    const cutoff = now - windowMs;
    timelineInvokes = timelineInvokes.filter((t) => t > cutoff);

    // Bucket invokes into ~60 columns
    const buckets = 60;
    const bucket = new Array(buckets).fill(0);
    for (const t of timelineInvokes) {
        const idx = Math.min(buckets - 1, Math.floor(((t - cutoff) / windowMs) * buckets));
        bucket[idx]++;
    }
    const max = Math.max(1, ...bucket);

    const barW = w / buckets;
    for (let i = 0; i < buckets; i++) {
        const v = bucket[i] / max;
        const barH = v * h;
        const alpha = 0.4 + v * 0.6;
        timelineCtx.fillStyle = `rgba(92, 213, 251, ${alpha})`;
        timelineCtx.fillRect(i * barW, h - barH, barW * 0.7, barH);
    }
}

// ===================== Lifecycle =====================

initScene(document.getElementById('viewport'));
wireStreamControls();
wireResize();
connect();

// Periodic UI tick: refresh relative times, stats, timeline.
setInterval(() => {
    renderSidebar();
    renderTasks();
    updateActiveCalls();
    drawTimeline();
}, 1000);
