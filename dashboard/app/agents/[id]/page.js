'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ReactFlow, Background, Controls, MarkerType } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Link from 'next/link';
import { getJSON, streamEvents } from '../../../lib/api';
import { statusesAtSeq, durationsByTask, criticalPath, aggregateEvents, tierCost } from '../../../lib/replay';

const STATUS_STYLE = {
  pending: { background: '#2a3142', border: '#5a6274', color: '#d7dce6' },
  running: { background: '#12344f', border: '#4a8fe0', color: '#bfd8f6' },
  done: { background: '#123322', border: '#3fb96a', color: '#c9f2d7' },
  failed: { background: '#3a1414', border: '#e05252', color: '#f6c9c9' },
  blocked: { background: '#3a2c0d', border: '#d9a03c', color: '#f4e3b8' },
};

function TaskNode({ data }) {
  const s = STATUS_STYLE[data.status] || STATUS_STYLE.pending;
  return (
    <div
      className={data.critical ? 'critical' : ''}
      style={{
        background: s.background, border: `1px solid ${s.border}`, borderRadius: 8,
        padding: '8px 12px', color: s.color, minWidth: 140, fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12,
      }}
    >
      <div style={{ fontWeight: 700 }}>{data.label}</div>
      <div style={{ opacity: 0.75 }}>{data.template}</div>
    </div>
  );
}

const nodeTypes = { task: TaskNode };

function layoutGraph(tasks) {
  // Layered layout by dependency depth.
  const byId = Object.fromEntries(tasks.map((t) => [t.id, t]));
  const level = {};
  const depth = (id) => {
    if (level[id] !== undefined) return level[id];
    const deps = (byId[id]?.depends_on || []).filter((d) => byId[d]);
    const max = deps.length ? Math.max(...deps.map(depth)) + 1 : 0;
    level[id] = max;
    return max;
  };
  tasks.forEach((t) => depth(t.id));
  const rows = {};
  tasks.forEach((t) => {
    (rows[level[t.id]] = rows[level[t.id]] || []).push(t);
  });
  const nodes = [];
  const edges = [];
  for (const [lvl, list] of Object.entries(rows)) {
    list.forEach((t, i) => {
      nodes.push({
        id: t.id,
        type: 'task',
        position: { x: Number(lvl) * 280, y: i * 110 },
        data: { label: t.name, template: t.template, status: t.status, critical: false },
      });
      for (const dep of t.depends_on || []) {
        if (byId[dep]) {
          edges.push({
            id: `${dep}->${t.id}`, source: dep, target: t.id,
            markerEnd: { type: MarkerType.ArrowClosed }, style: { stroke: '#5a6274' },
          });
        }
      }
    });
  }
  return { nodes, edges, byId };
}

export default function RunExplorer({ params }) {
  const agentId = params.id;
  const [agent, setAgent] = useState(null);
  const [tasks, setTasks] = useState([]);
  const [events, setEvents] = useState([]);
  const [artifacts, setArtifacts] = useState([]);
  const [selected, setSelected] = useState(null);
  const [showCritical, setShowCritical] = useState(false);
  const [logTail, setLogTail] = useState([]);
  const [transcript, setTranscript] = useState(null);
  const [error, setError] = useState(null);

  const [seq, setSeq] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [followLive, setFollowLive] = useState(true);
  const playRef = useRef(null);
  const maxSeq = events.length ? events[events.length - 1].seq : 0;

  useEffect(() => {
    Promise.all([
      getJSON(`/agents/${agentId}`),
      getJSON(`/agents/${agentId}/graph`),
      getJSON(`/agents/${agentId}/events`),
      getJSON(`/agents/${agentId}/artifacts`),
    ])
      .then(([a, g, ev, ar]) => {
        setAgent(a);
        setTasks(g.tasks);
        setEvents(ev.events);
        setArtifacts(ar.artifacts);
        setSeq(ev.events.length ? ev.events[ev.events.length - 1].seq : 0);
        setLogTail(ev.events.slice(-40));
      })
      .catch((e) => setError(e.message));
  }, [agentId]);

  useEffect(() => {
    const close = streamEvents(
      agentId,
      (e) => {
        setLogTail((tail) => [...tail.slice(-200), e]);
        if (followLive) setSeq(e.seq);
      },
      () => {}
    );
    return close;
  }, [agentId, followLive]);

  useEffect(() => {
    if (playing) {
      playRef.current = setInterval(() => {
        setSeq((s) => {
          if (s >= maxSeq) {
            setPlaying(false);
            return s;
          }
          return s + 1;
        });
      }, 120);
    }
    return () => clearInterval(playRef.current);
  }, [playing, maxSeq]);

  const graph = useMemo(() => layoutGraph(tasks), [tasks]);
  const taskIds = useMemo(() => new Set(tasks.map((t) => t.id)), [tasks]);
  const depsByTask = useMemo(() => {
    const m = {};
    tasks.forEach((t) => (m[t.id] = t.depends_on || []));
    return m;
  }, [tasks]);

  const statuses = useMemo(() => statusesAtSeq(events, taskIds, depsByTask, seq), [events, taskIds, depsByTask, seq]);
  const durations = useMemo(() => durationsByTask(events), [events]);
  const crit = useMemo(() => (showCritical ? criticalPath(tasks, durations) : new Set()), [tasks, durations, showCritical]);
  const metrics = useMemo(() => aggregateEvents(events), [events]);

  const nodes = useMemo(
    () =>
      graph.nodes.map((n) => ({
        ...n,
        data: { ...n.data, status: statuses[n.id] || 'pending', critical: crit.has(n.id) },
      })),
    [graph, statuses, crit]
  );

  const onNodeClick = useCallback((_, node) => {
    setSelected(node.data.label);
    setTranscript(null);
  }, []);
  const onNodeDoubleClick = useCallback((_, node) => setSelected(node.data.label), []);

  const selTask = tasks.find((t) => t.name === selected);
  const selMetrics = selTask ? metrics[selTask.id] : null;

  const selArtifacts = selTask ? artifacts.filter((a) => a.task_id === selTask.id) : [];
  const totalTokens = Object.values(metrics).reduce((a, b) => a + b.tokens, 0);
  const totalTools = Object.values(metrics).reduce((a, b) => a + b.tools, 0);

  const step = (delta) => setSeq((s) => Math.max(0, Math.min(maxSeq, s + delta)));

  if (error) return <main><p>Failed to load run: {error}</p><p className="muted">Is the control plane running at {process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}?</p></main>;

  return (
    <main>
      <header className="topbar" style={{ margin: '-20px -20px 16px' }}>
        <h1><Link href="/">← Agents</Link> &nbsp; Run Explorer</h1>
        {agent && <span className="mono muted">{agent.id}</span>}
        {agent && <span className={`pill ${agent.status}`}>{agent.status}</span>}
        <div className="status-pill muted mono">seq {seq}/{maxSeq}</div>
      </header>

      <div className="toolbar">
        <button onClick={() => { setPlaying((p) => !p); setFollowLive(false); }}>
          {playing ? '⏸ Pause' : '▶ Play'}
        </button>
        <button onClick={() => step(-1)}>⏮ Step back</button>
        <button onClick={() => step(1)}>⏭ Step forward</button>
        <input
          type="range" min={0} max={maxSeq} value={seq}
          onChange={(e) => { setSeq(Number(e.target.value)); setFollowLive(false); setPlaying(false); }}
        />
        <label><input type="checkbox" checked={followLive} onChange={(e) => setFollowLive(e.target.checked)} /> follow live</label>
        <label><input type="checkbox" checked={showCritical} onChange={(e) => setShowCritical(e.target.checked)} /> critical path</label>
        <span className="muted mono">{totalTokens} tokens · {totalTools} tool calls</span>
      </div>

      <div className="explorer">
        <div className="graph-panel">
          <div className="panel">
            <h2>Execution Graph</h2>
            <div className="graph-box">
              <ReactFlow nodes={nodes} edges={graph.edges} nodeTypes={nodeTypes} onNodeClick={onNodeClick} fitView>
                <Background gap={20} />
                <Controls />
              </ReactFlow>
            </div>
            <div className="note">Click a node for details · amber outline = critical path (longest real duration)</div>
          </div>

          <div className="panel">
            <h2>Event Log</h2>
            <div className="eventlog">
              {logTail.map((e, i) => (
                <div key={`${e.seq}-${i}`}><span className="seq">#{e.seq}</span>{e.type} <span className="muted">{e.task_id ? e.task_id.split('_').slice(-1)[0] : ''}</span></div>
              ))}
            </div>
          </div>
        </div>

        <div className="side">
          <div className="panel details">
            <h2>Task Details</h2>
            <div className="body">
              {!selTask ? (
                <p className="muted">Select a node in the graph.</p>
              ) : (
                <dl>
                  <dt>task</dt><dd>{selTask.name}</dd>
                  <dt>template</dt><dd>{selTask.template}</dd>
                  <dt>status</dt><dd><span className={`pill ${statuses[selTask.id] || selTask.status}`}>{statuses[selTask.id] || selTask.status}</span></dd>
                  <dt>worker</dt><dd>{events.find((e) => e.task_id === selTask.id && e.type === 'task_started')?.payload?.worker || '—'}</dd>
                  <dt>attempts</dt><dd>{selTask.attempt}/{selTask.max_attempt}</dd>
                  <dt>duration</dt><dd>{durations[selTask.id] != null ? `${durations[selTask.id]} ms` : '—'}</dd>
                  <dt>model</dt><dd>{selMetrics?.model || '—'}</dd>
                  <dt>tokens</dt><dd>{selMetrics?.tokens ?? 0}</dd>
                  <dt>cost</dt><dd>{selMetrics ? `$${tierCost(selMetrics.model?.split('/')[0] || 'coding', selMetrics.tokens)}` : '—'}</dd>
                  <dt>tool calls</dt><dd>{selMetrics?.tools ?? 0}</dd>
                  <dt>retries</dt><dd>{selMetrics?.retries ?? 0}</dd>
                  <dt>error</dt><dd className="muted">{selTask.error || '—'}</dd>
                </dl>
              )}
            </div>
          </div>

          <div className="panel">
            <h2>Artifacts & Logs</h2>
            <div className="body artifacts">
              {artifacts.length === 0 && <p className="muted">No artifacts.</p>}
              <ul style={{ paddingLeft: 16 }}>
                {artifacts.map((a) => (
                  <li key={a.id}>
                    <button
                      onClick={() =>
                        getJSON(`/agents/${agentId}/artifacts/${a.id}/content`)
                          .then((c) => setTranscript({ name: a.path, content: c }))
                          .catch((e) => setTranscript({ name: a.path, content: `error: ${e.message}` }))
                      }
                    >
                      [{a.type}] {a.path.split('/').slice(-1)[0]}
                    </button>
                    <span className="muted"> · {(a.size / 1024).toFixed(1)} KB</span>
                  </li>
                ))}
              </ul>
              {transcript && (
                <>
                  <div className="muted mono" style={{ margin: '8px 0 4px' }}>{transcript.name}</div>
                  <pre className="transcript">{typeof transcript.content === 'string' ? transcript.content : JSON.stringify(transcript.content, null, 2)}</pre>
                </>
              )}
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}
