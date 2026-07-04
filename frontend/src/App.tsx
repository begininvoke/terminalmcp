import { useEffect, useReducer, useRef, useState, type ReactNode } from "react";
import { PromptEntry } from "./components/PromptEntry";
import { Roadmap } from "./components/Roadmap";
import { TerminalView } from "./components/TerminalView";
import { Findings } from "./components/Findings";
import { ActivityFeed, type TimelineEntry } from "./components/ActivityFeed";
import { IntakeDialog } from "./components/IntakeDialog";
import { ReportView } from "./components/ReportView";
import { Settings } from "./components/Settings";
import { LogsView, type LogLine } from "./components/LogsView";
import { History } from "./components/History";
import { Discovered } from "./components/Discovered";
import { Notes } from "./components/Notes";
import { StepProgress } from "./components/StepProgress";
import { Button } from "./components/ui/button";
import { Badge } from "./components/ui/badge";
import { createEngagement, getEngagement, openEventStream, sendMessage, stopEngagement } from "./api";
import type { CommandRun, Engagement, Finding, Report, RoadmapStep, WsEvent } from "./types";
import { ShieldAlert, Square, Plus, Terminal, ScrollText, Settings2, History as HistoryIcon, Compass, NotebookPen } from "lucide-react";

type View = "engagement" | "discovered" | "notes" | "history" | "logs" | "settings";

interface State {
  status: string;
  phase: string;
  roadmap: RoadmapStep[];
  commands: CommandRun[];
  findings: Finding[];
  timeline: TimelineEntry[];
  log: LogLine[];
  question: { prompt?: string; questions: string[]; options?: string[] } | null;
  report: Report | null;
  activeStepId?: string;
}

const initialState: State = {
  status: "created",
  phase: "intake",
  roadmap: [],
  commands: [],
  findings: [],
  timeline: [],
  log: [],
  question: null,
  report: null,
};

function toLogLine(ev: WsEvent): LogLine {
  const d = ev.data || {};
  let text = "";
  switch (ev.type) {
    case "command_started": text = d.cmdline; break;
    case "output_chunk": text = `[${d.stream}] ${d.data}`; break;
    case "command_finished": text = `exit ${d.exit_code}`; break;
    case "finding": text = `${d.severity}: ${d.title}`; break;
    case "analysis": text = `(${d.risk}) ${d.summary}`; break;
    case "recommendation": text = (d.next_actions || []).map((a: any) => a.title).join("; "); break;
    case "agent_message": text = d.text; break;
    case "awaiting_input": text = (d.questions || []).join(" | "); break;
    case "roadmap_updated": text = `${(d.steps || []).length} steps`; break;
    case "step_begin": text = d.title; break;
    case "phase_changed": text = d.phase; break;
    case "status": text = d.status; break;
    case "report_finalized": text = "report ready"; break;
    case "error": text = d.message; break;
    default: text = JSON.stringify(d);
  }
  let ts = "";
  try { ts = new Date(ev.ts).toLocaleTimeString(); } catch { ts = ""; }
  return { ts, type: ev.type, text: text || "" };
}

function reduceEvent(state: State, ev: WsEvent): State {
  const d = ev.data || {};
  switch (ev.type) {
    case "status":
      return { ...state, status: d.status, question: d.status === "awaiting_input" ? state.question : null };
    case "phase_changed":
      return { ...state, phase: d.phase };
    case "roadmap_updated":
      return { ...state, roadmap: d.steps || [] };
    case "step_begin":
      return { ...state, activeStepId: ev.step_id, timeline: [...state.timeline, { kind: "step", stepId: ev.step_id || "", title: d.title, rationale: d.rationale }] };
    case "command_started":
      return { ...state, commands: [...state.commands, { cid: d.cid, cmdline: d.cmdline, lines: [], done: false }] };
    case "output_chunk":
      return { ...state, commands: state.commands.map((c) => (c.cid === d.cid ? { ...c, lines: [...c.lines, { stream: d.stream, data: d.data }] } : c)) };
    case "command_finished":
      return { ...state, commands: state.commands.map((c) => (c.cid === d.cid ? { ...c, done: true, exitCode: d.exit_code } : c)) };
    case "analysis":
      return { ...state, timeline: [...state.timeline, { kind: "analysis", stepId: ev.step_id || "", summary: d.summary, risk: d.risk }] };
    case "finding":
      return { ...state, findings: [...state.findings, d as Finding] };
    case "recommendation":
      return { ...state, timeline: [...state.timeline, { kind: "recommendation", stepId: ev.step_id || "", actions: d.next_actions || [] }] };
    case "agent_message":
      return { ...state, timeline: [...state.timeline, { kind: "message", text: d.text }] };
    case "awaiting_input":
      return { ...state, question: { prompt: d.prompt, questions: d.questions || [], options: d.options } };
    case "report_finalized":
      return { ...state, report: d as Report };
    case "error":
      return { ...state, timeline: [...state.timeline, { kind: "message", text: "⚠ " + (d.message || "error") }] };
    default:
      return state;
  }
}

function reducer(state: State, ev: WsEvent): State {
  if (ev.type === "__reset") return initialState;
  if (ev.type?.startsWith("__")) return state;
  const next = reduceEvent(state, ev);
  return { ...next, log: [...state.log, toLogLine(ev)] };
}

export default function App() {
  const [engagement, setEngagement] = useState<Engagement | null>(null);
  const [starting, setStarting] = useState(false);
  const [view, setView] = useState<View>("engagement");
  const [state, dispatch] = useReducer(reducer, initialState);
  const wsRef = useRef<WebSocket | null>(null);

  function connect(eng: Engagement) {
    wsRef.current?.close();
    dispatch({ type: "__reset" } as WsEvent);
    setEngagement(eng);
    window.location.hash = `e=${eng.id}`;
    wsRef.current = openEventStream(eng.id, (ev) => dispatch(ev));
  }

  // Deep link: open the engagement named in the URL hash on first load.
  useEffect(() => {
    const m = window.location.hash.match(/e=([^&]+)/);
    if (m) {
      getEngagement(m[1]).then(connect).catch(() => (window.location.hash = ""));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleStart(name: string, prompt: string, goal?: string, cookie?: string, squad?: string[]) {
    setStarting(true);
    try {
      connect(await createEngagement(name, prompt, { goal, cookie, squad }));
      setView("engagement");
    } catch {
      alert("Failed to start engagement. Is the backend running on :8080?");
    } finally {
      setStarting(false);
    }
  }

  async function handleOpen(id: string) {
    try {
      connect(await getEngagement(id));
      setView("engagement");
    } catch {
      alert("Failed to open engagement.");
    }
  }

  async function handleReply(text: string) {
    if (engagement) await sendMessage(engagement.id, text);
  }
  async function handleStop() {
    if (engagement) await stopEngagement(engagement.id);
  }
  function handleReset() {
    wsRef.current?.close();
    wsRef.current = null;
    dispatch({ type: "__reset" } as WsEvent);
    setEngagement(null);
    window.location.hash = "";
    setView("engagement");
  }

  return (
    <div className="min-h-screen">
      {view === "engagement" && state.question && (
        <IntakeDialog prompt={state.question.prompt} questions={state.question.questions} options={state.question.options} onSubmit={handleReply} />
      )}

      <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur">
        <div className="flex items-center justify-between px-4 py-2.5">
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <ShieldAlert className="h-5 w-5 text-primary" />
              <span className="text-sm font-semibold">AI Black-Box Pentest</span>
            </div>
            <nav className="flex items-center gap-1">
              <NavBtn active={view === "engagement"} onClick={() => setView("engagement")} icon={<Terminal className="h-3.5 w-3.5" />}>Engagement</NavBtn>
              <NavBtn active={view === "discovered"} onClick={() => setView("discovered")} icon={<Compass className="h-3.5 w-3.5" />}>Discovered</NavBtn>
              <NavBtn active={view === "notes"} onClick={() => setView("notes")} icon={<NotebookPen className="h-3.5 w-3.5" />}>Notes</NavBtn>
              <NavBtn active={view === "history"} onClick={() => setView("history")} icon={<HistoryIcon className="h-3.5 w-3.5" />}>History</NavBtn>
              <NavBtn active={view === "logs"} onClick={() => setView("logs")} icon={<ScrollText className="h-3.5 w-3.5" />}>Logs</NavBtn>
              <NavBtn active={view === "settings"} onClick={() => setView("settings")} icon={<Settings2 className="h-3.5 w-3.5" />}>Settings</NavBtn>
            </nav>
          </div>
          {engagement && (
            <div className="flex items-center gap-2">
              <Badge className="border-border bg-muted text-muted-foreground">{engagement.name}</Badge>
              {engagement.goal && <Badge className="border-primary/40 bg-primary/10 text-primary">🎯 {engagement.goal}</Badge>}
              <span className="font-mono text-[11px] text-muted-foreground">{engagement.id}</span>
              <Badge className="border-primary/40 bg-primary/10 text-primary">{state.status}</Badge>
              <Button variant="destructive" size="sm" onClick={handleStop} disabled={["completed", "incomplete", "done", "stopped", "interrupted", "error"].includes(state.status)}>
                <Square className="h-3.5 w-3.5" /> Stop
              </Button>
              <Button variant="outline" size="sm" onClick={handleReset}><Plus className="h-3.5 w-3.5" /> New</Button>
            </div>
          )}
        </div>
      </header>

      {view === "settings" && <Settings />}
      {view === "discovered" && <Discovered engagementId={engagement?.id} />}
      {view === "notes" && <Notes engagementId={engagement?.id} />}
      {view === "history" && <History onOpen={handleOpen} />}
      {view === "logs" && <LogsView live={state.log} engagementId={engagement?.id} />}
      {view === "engagement" && !engagement && <PromptEntry onStart={handleStart} starting={starting} />}
      {view === "engagement" && engagement && (
        <main className="grid grid-cols-1 gap-4 p-4 lg:grid-cols-12">
          <div className="lg:col-span-3">
            <Roadmap steps={state.roadmap} activeStepId={state.activeStepId} />
          </div>
          <div className="space-y-4 lg:col-span-6">
            <StepProgress roadmap={state.roadmap} phase={state.phase} />
            <ActivityFeed entries={state.timeline} />
            <TerminalView commands={state.commands} />
            {state.report && <ReportView report={state.report} />}
          </div>
          <div className="lg:col-span-3">
            <Findings findings={state.findings} />
          </div>
        </main>
      )}
    </div>
  );
}

function NavBtn({ active, onClick, icon, children }: { active: boolean; onClick: () => void; icon: ReactNode; children: ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors ${
        active ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      }`}
    >
      {icon}
      {children}
    </button>
  );
}
