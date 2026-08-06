export const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

export async function getJSON(path) {
  const res = await fetch(`${API}${path}`);
  if (!res.ok) throw new Error(`GET ${path}: ${res.status}`);
  return res.json();
}

// streamEvents tails the agent event stream. Returns a close function.
export function streamEvents(agentId, onEvent, onError) {
  const es = new EventSource(`${API}/agents/${agentId}/events/stream`);
  es.onmessage = (e) => {
    try {
      onEvent(JSON.parse(e.data));
    } catch (_) {}
  };
  es.onerror = onError;
  return () => es.close();
}
