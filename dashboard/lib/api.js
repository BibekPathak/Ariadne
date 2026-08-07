export const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const KEY_STORAGE = 'adriane_api_key';

export function getKey() {
  if (typeof window === 'undefined') return null;
  return window.localStorage.getItem(KEY_STORAGE);
}

export function setKey(k) {
  window.localStorage.setItem(KEY_STORAGE, k);
}

export function clearKey() {
  window.localStorage.removeItem(KEY_STORAGE);
}

async function headers() {
  const h = { 'Content-Type': 'application/json' };
  const k = getKey();
  if (k) h['Authorization'] = `Bearer ${k}`;
  return h;
}

export async function getJSON(path) {
  const res = await fetch(`${API}${path}`, { headers: await headers() });
  if (res.status === 401) {
    clearKey();
    throw new Error('unauthorized');
  }
  if (!res.ok) throw new Error(`GET ${path}: ${res.status}`);
  return res.json();
}

// streamEvents tails the agent event stream via fetch (EventSource cannot send
// the Authorization header). Returns a close function.
export function streamEvents(agentId, onEvent, onError) {
  let closed = false;
  const controller = new AbortController();
  async function run() {
    try {
      const res = await fetch(`${API}/agents/${agentId}/events/stream`, {
        headers: await headers(),
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        onError && onError(new Error(`stream ${res.status}`));
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      while (!closed) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        let idx;
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const chunk = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          const line = chunk.split('\n').find((l) => l.startsWith('data: '));
          if (line) {
            try {
              onEvent(JSON.parse(line.slice(6)));
            } catch (_) {}
          }
        }
      }
    } catch (e) {
      if (!closed) onError && onError(e);
    }
  }
  run();
  return () => {
    closed = true;
    controller.abort();
  };
}
