import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Button } from "./ui/button";
import { getLogs, getLLMLog } from "../api";
import { ScrollText, RefreshCw, Bot } from "lucide-react";

export interface LogLine {
  ts: string;
  type: string;
  text: string;
}

const typeColor: Record<string, string> = {
  command_started: "text-emerald-400",
  output_chunk: "text-slate-400",
  command_finished: "text-emerald-300",
  finding: "text-red-300",
  analysis: "text-amber-300",
  error: "text-red-400",
  awaiting_input: "text-blue-300",
  agent_message: "text-blue-300",
};

export function LogsView({ live, engagementId }: { live: LogLine[]; engagementId?: string }) {
  const [audit, setAudit] = useState<string[]>([]);
  const [auditPath, setAuditPath] = useState("");
  const [llm, setLlm] = useState<any[]>([]);
  const endRef = useRef<HTMLDivElement>(null);

  async function refresh() {
    try {
      const r = await getLogs();
      setAudit(r.lines);
      setAuditPath(r.path);
    } catch {
      /* ignore */
    }
    if (engagementId) {
      try {
        const r = await getLLMLog(engagementId);
        setLlm(r.entries || []);
      } catch {
        /* ignore */
      }
    }
  }
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [engagementId]);
  useEffect(() => {
    endRef.current?.scrollIntoView();
  }, [live]);

  return (
    <div className="space-y-4 p-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card className="flex flex-col">
          <CardHeader className="pb-2">
            <div className="flex items-center gap-2">
              <ScrollText className="h-4 w-4 text-primary" />
              <CardTitle>Live event log</CardTitle>
            </div>
          </CardHeader>
          <CardContent>
            <div className="h-80 overflow-y-auto scroll-thin rounded-md border bg-black/50 p-3 font-mono text-xs">
              {live.length === 0 && <div className="text-muted-foreground">No events yet.</div>}
              {live.map((l, i) => (
                <div key={i} className="flex gap-2">
                  <span className="shrink-0 text-muted-foreground">{l.ts}</span>
                  <span className={`shrink-0 ${typeColor[l.type] || "text-slate-300"}`}>{l.type}</span>
                  <span className="min-w-0 break-all text-slate-300">{l.text}</span>
                </div>
              ))}
              <div ref={endRef} />
            </div>
          </CardContent>
        </Card>

        <Card className="flex flex-col">
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Command audit log</CardTitle>
              <Button size="sm" variant="outline" onClick={refresh}>
                <RefreshCw className="h-3.5 w-3.5" /> Refresh
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{auditPath || "audit.log"} — every command executed.</p>
          </CardHeader>
          <CardContent>
            <div className="h-80 overflow-y-auto scroll-thin rounded-md border bg-black/50 p-3 font-mono text-xs">
              {audit.length === 0 && <div className="text-muted-foreground">No commands logged yet.</div>}
              {audit.map((l, i) => (
                <div key={i} className="break-all text-slate-300">{l}</div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center gap-2">
            <Bot className="h-4 w-4 text-primary" />
            <CardTitle>AI requests &amp; responses</CardTitle>
          </div>
          <p className="text-xs text-muted-foreground">
            Every prompt sent to the model and its reply, saved to <code>{engagementId || "&lt;id&gt;"}.llm.jsonl</code>. {llm.length} round-trips.
          </p>
        </CardHeader>
        <CardContent>
          {!engagementId && <p className="text-xs text-muted-foreground">Open an engagement to see its AI log.</p>}
          {engagementId && llm.length === 0 && <p className="text-xs text-muted-foreground">No AI round-trips logged yet.</p>}
          <div className="space-y-2">
            {llm.map((e, i) => <LLMEntry key={i} entry={e} idx={i} />)}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function LLMEntry({ entry, idx }: { entry: any; idx: number }) {
  const [open, setOpen] = useState(false);
  const msgs = entry?.request?.messages || [];
  const last = msgs[msgs.length - 1];
  const lastText = last?.content?.map?.((b: any) => b.text || (b.type === "tool_result" ? "[tool_result]" : "")).join(" ").slice(0, 120);
  const resp = entry?.response;
  const respText = (resp?.Content || []).map((b: any) => b.text || (b.type === "tool_use" ? `→ ${b.name}()` : "")).join(" ").slice(0, 160);
  const stop = resp?.StopReason;

  return (
    <div className="rounded-md border">
      <button onClick={() => setOpen((o) => !o)} className="flex w-full items-center justify-between gap-2 p-2 text-left">
        <span className="font-mono text-[11px] text-muted-foreground">#{idx + 1} · {String(entry.ts).slice(11, 19)} · {msgs.length} msgs · {stop || (entry.error ? "ERROR" : "")}</span>
        <span className="min-w-0 flex-1 truncate text-xs text-slate-300">{entry.error ? "⚠ " + entry.error : respText || lastText}</span>
      </button>
      {open && (
        <div className="space-y-2 border-t p-2">
          {entry.error && <pre className="whitespace-pre-wrap rounded bg-red-500/10 p-2 text-[11px] text-red-300">{entry.error}</pre>}
          <div className="text-[10px] font-semibold uppercase text-muted-foreground">Request ({msgs.length} messages)</div>
          <pre className="max-h-60 overflow-auto scroll-thin whitespace-pre-wrap break-words rounded bg-black/50 p-2 font-mono text-[10px]">{JSON.stringify(entry.request, null, 2)}</pre>
          <div className="text-[10px] font-semibold uppercase text-muted-foreground">Response</div>
          <pre className="max-h-60 overflow-auto scroll-thin whitespace-pre-wrap break-words rounded bg-black/50 p-2 font-mono text-[10px]">{JSON.stringify(entry.response, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}
