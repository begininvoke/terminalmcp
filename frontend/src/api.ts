import type { Engagement } from "./types";

const API_BASE = "http://localhost:8080";
const WS_BASE = "ws://localhost:8080";

export async function createEngagement(name: string, firstPrompt: string, opts?: { cookie?: string; goal?: string; squad?: string[] }): Promise<Engagement> {
  const res = await fetch(`${API_BASE}/api/engagements`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name, first_prompt: firstPrompt, cookie: opts?.cookie || "", goal: opts?.goal || "", squad: opts?.squad || [] }),
  });
  if (!res.ok) throw new Error(`create failed: ${res.status}`);
  return res.json();
}

export async function listEngagements(): Promise<any[]> {
  const res = await fetch(`${API_BASE}/api/engagements`);
  if (!res.ok) throw new Error(`list failed: ${res.status}`);
  return res.json();
}

export async function getEngagement(id: string): Promise<Engagement> {
  const res = await fetch(`${API_BASE}/api/engagements/${id}`);
  if (!res.ok) throw new Error(`get failed: ${res.status}`);
  return res.json();
}

export async function sendMessage(id: string, text: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/engagements/${id}/message`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ text }),
  });
  if (!res.ok) throw new Error(`message failed: ${res.status}`);
}

export async function stopEngagement(id: string): Promise<void> {
  await fetch(`${API_BASE}/api/engagements/${id}/stop`, { method: "POST" });
}

export async function getConfig(): Promise<any> {
  const res = await fetch(`${API_BASE}/api/config`);
  if (!res.ok) throw new Error(`config failed: ${res.status}`);
  return res.json();
}

export async function updateConfig(cfg: any): Promise<any> {
  const res = await fetch(`${API_BASE}/api/config`, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) throw new Error(`save failed: ${res.status}`);
  return res.json();
}

export async function getLLMLog(id: string): Promise<{ entries: any[] }> {
  const res = await fetch(`${API_BASE}/api/engagements/${id}/llm`);
  if (!res.ok) throw new Error(`llm log failed: ${res.status}`);
  return res.json();
}

export async function getLogs(): Promise<{ path: string; lines: string[] }> {
  const res = await fetch(`${API_BASE}/api/logs`);
  if (!res.ok) throw new Error(`logs failed: ${res.status}`);
  return res.json();
}

export function openEventStream(id: string, onEvent: (ev: any) => void): WebSocket {
  const ws = new WebSocket(`${WS_BASE}/ws?engagement=${id}`);
  ws.onmessage = (m) => {
    try {
      onEvent(JSON.parse(m.data));
    } catch {
      /* ignore malformed */
    }
  };
  return ws;
}
