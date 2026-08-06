'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { getJSON } from '../lib/api';

function fmtDate(iso) {
  return new Date(iso).toLocaleString();
}

export default function AgentsPage() {
  const [agents, setAgents] = useState([]);
  const [error, setError] = useState(null);

  useEffect(() => {
    getJSON('/agents')
      .then((d) => setAgents(d.agents))
      .catch((e) => setError(e.message));
  }, []);

  if (error) return <main><p>Failed to load agents: {error}</p><p>Is the control plane running at <span className="mono">{process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}</span>?</p></main>;

  return (
    <main>
      <h1>Agents</h1>
      {agents.length === 0 && <p className="muted">No runs yet. Create one with <span className="mono">make demo</span>.</p>}
      <table>
        <thead>
          <tr><th>id</th><th>template</th><th>goal</th><th>status</th><th>created</th></tr>
        </thead>
        <tbody>
          {agents.map((a) => (
            <tr key={a.id}>
              <td className="mono"><Link href={`/agents/${a.id}`}>{a.id}</Link></td>
              <td>{a.template}</td>
              <td style={{ maxWidth: 420, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.goal}</td>
              <td><span className={`pill ${a.status}`}>{a.status}</span></td>
              <td className="muted">{fmtDate(a.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </main>
  );
}
