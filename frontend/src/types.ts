export type Risk = "info" | "low" | "medium" | "high" | "critical";

export interface RoadmapStep {
  id: string;
  phase: string;
  title: string;
  intent?: string;
  status: "pending" | "running" | "done" | "skipped" | "failed";
}

export interface Finding {
  id: string;
  step_id?: string;
  title: string;
  severity: Risk;
  confidence?: "confirmed" | "suspected" | "false_positive";
  type?: string;
  cwe?: string;
  owasp?: string;
  cvss?: string;
  target: string;
  endpoint?: string;
  parameter?: string;
  description?: string;
  impact?: string;
  steps_to_reproduce?: string[];
  request?: string;
  response?: string;
  evidence?: string;
  remediation?: string;
  references?: string[];
}

export interface NextAction {
  title: string;
  why?: string;
}

export interface CommandRun {
  cid: string;
  cmdline: string;
  lines: { stream: string; data: string }[];
  exitCode?: number;
  done: boolean;
}

export interface StepInfo {
  id: string;
  title: string;
  rationale?: string;
  analysis?: { summary: string; risk: Risk };
}

export interface Report {
  executive_summary: string;
  methodology?: string;
  scope?: string;
}

export interface Engagement {
  id: string;
  name: string;
  first_prompt: string;
  goal?: string;
  notes?: string;
  status: string;
  phase: string;
}

// WebSocket event envelope from the backend.
export interface WsEvent {
  type: string;
  step_id?: string;
  data?: any;
  ts: string;
}
