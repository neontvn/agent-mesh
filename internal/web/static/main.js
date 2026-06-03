import { initScene, syncAgents, animateInvoke, setHealth } from './scene.js';

// ===================== State =====================

// agent_id -> { id, capabilities, health, lastSeen, endpoint }
const agents = new Map();

// Sliding window of recent invoke timestamps (for "Active calls/sec").
let recentInvokes = [];

// Sliding window of invoke timestamps for the timeline histogram.
let timelineInvokes = [];

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
            const a = agents.get(d.agent_id);
            if (a) {
                a.lastSeen = ts;
                if (d.health) a.health = d.health;
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
                animateInvoke(d.caller_id, d.callee_id);
            }
            break;
        }

        default:
            break;
    }

    renderSidebar();
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
connect();

// Periodic UI tick: refresh relative times, stats, timeline.
setInterval(() => {
    renderSidebar();
    updateActiveCalls();
    drawTimeline();
}, 1000);
