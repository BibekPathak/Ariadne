'use client';

import { useEffect, useState } from 'react';
import { getKey, setKey, clearKey, getJSON } from '../lib/api';

// Gates the app behind an API-key login. The key is stored in localStorage and
// sent as Authorization: Bearer on every request.
export default function AuthGate({ children }) {
  const [state, setState] = useState('loading'); // loading | login | ok
  const [keyInput, setKeyInput] = useState('');

  useEffect(() => {
    if (!getKey()) {
      setState('login');
      return;
    }
    getJSON('/me')
      .then(() => setState('ok'))
      .catch(() => {
        clearKey();
        setState('login');
      });
  }, []);

  if (state === 'loading') return <main className="muted">loading…</main>;

  if (state === 'login') {
    return (
      <main style={{ maxWidth: 420, margin: '80px auto' }}>
        <h1>Adriane</h1>
        <p className="muted">Enter your API key to open the Run Explorer.</p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!keyInput.trim()) return;
            setKey(keyInput.trim());
            setState('loading');
            getJSON('/me')
              .then(() => setState('ok'))
              .catch(() => {
                clearKey();
                setState('login');
              });
          }}
        >
          <input
            type="password"
            placeholder="adr_…"
            value={keyInput}
            onChange={(e) => setKeyInput(e.target.value)}
            style={{ width: '100%', padding: 8, margin: '8px 0', background: 'var(--panel-2)', color: 'var(--text)', border: '1px solid var(--border)', borderRadius: 6 }}
          />
          <button type="submit" style={{ width: '100%', padding: 8, cursor: 'pointer', background: 'var(--panel-2)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text)' }}>
            Open Explorer
          </button>
        </form>
        <p className="muted" style={{ marginTop: 12, fontSize: 12 }}>
          Default dev key: <code>adr-dev-admin</code>. Mint keys via <code>POST /auth/keys</code>.
        </p>
      </main>
    );
  }

  return <>{children}</>;
}
