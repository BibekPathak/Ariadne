// Replay + critical-path helpers. Events are the source of truth for what
// happened when; node state is reconstructed by replaying events up to a seq.

const STATUS_MAP = {
  task_started: 'running',
  task_finished: 'done',
  task_failed: 'failed',
};

// statusesAtSeq returns { taskId: status } by replaying events up to seq.
// Pending tasks whose dependency failed are marked blocked.
export function statusesAtSeq(events, taskIds, depsByTask, seq) {
  const status = {};
  for (const e of events) {
    if (e.seq > seq) break;
    const t = e.task_id;
    if (!t || !taskIds.has(t)) continue;
    if (STATUS_MAP[e.type]) status[t] = STATUS_MAP[e.type];
  }
  // Blocked: still-pending task with a failed (or blocked) dependency.
  for (const tid of taskIds) {
    if (status[tid]) continue;
    const deps = depsByTask[tid] || [];
    if (deps.some((d) => status[d] === 'failed' || status[d] === 'blocked')) {
      status[tid] = 'blocked';
    }
  }
  return status;
}

// durationsByTask returns { taskId: ms } from the event timestamps.
export function durationsByTask(events) {
  const started = {};
  const dur = {};
  for (const e of events) {
    const t = e.task_id;
    if (!t) continue;
    if (e.type === 'task_started') started[t] = new Date(e.ts).getTime();
    else if (e.type === 'task_finished' || e.type === 'task_failed') {
      if (started[t]) dur[t] = new Date(e.ts).getTime() - started[t];
    }
  }
  return dur;
}

// criticalPath returns a Set of task ids on the longest-duration path.
export function criticalPath(tasks, durations) {
  const byId = Object.fromEntries(tasks.map((t) => [t.id, t]));
  const memo = {};
  const best = (tid) => {
    if (memo[tid]) return memo[tid];
    const t = byId[tid];
    let bestDep = null;
    let bestLen = 0;
    for (const dep of t.depends_on || []) {
      const p = best(dep);
      if (p.total > bestLen) {
        bestLen = p.total;
        bestDep = p;
      }
    }
    memo[tid] = {
      total: (durations[tid] || 0) + bestLen,
      nodes: [...(bestDep ? bestDep.nodes : []), tid],
    };
    return memo[tid];
  };
  let winner = null;
  for (const t of tasks) {
    const p = best(t.id);
    if (!winner || p.total > winner.total) winner = p;
  }
  return winner ? new Set(winner.nodes) : new Set();
}

// aggregateEventMetrics derives per-task and totals from the event stream.
export function aggregateEvents(events) {
  const perTask = {};
  for (const e of events) {
    const t = e.task_id || '';
    const bucket = (perTask[t] = perTask[t] || { tokens: 0, cost: 0, tools: 0, retries: 0, model: null });
    if (e.type === 'llm_called') {
      const tok = e.payload.total_tokens || 0;
      bucket.tokens += tok;
    } else if (e.type === 'tool_called') {
      bucket.tools += 1;
    } else if (e.type === 'retry_scheduled') {
      bucket.retries += 1;
    } else if (e.type === 'model_routed') {
      bucket.model = e.payload.model || `${e.payload.tier}/${e.payload.gateway}`;
    }
  }
  return perTask;
}

// pricing mirrors the backend placeholder blended cost per 1k tokens.
const PRICING = { fast: 0.001, coding: 0.003, reasoning: 0.01 };
export function tierCost(tier, tokens) {
  return ((tokens / 1000) * (PRICING[tier] || PRICING.coding)).toFixed(4);
}
