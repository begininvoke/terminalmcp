import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Button } from "./ui/button";
import { Progress } from "./ui/progress";
import { Badge } from "./ui/badge";
import { listEngagements } from "../api";
import { roadmapProgress } from "@/lib/utils";
import { History as HistoryIcon, RefreshCw, ExternalLink, Bug } from "lucide-react";

const statusColor: Record<string, string> = {
  running: "border-blue-500/30 bg-blue-500/15 text-blue-300",
  awaiting_input: "border-amber-500/30 bg-amber-500/15 text-amber-300",
  completed: "border-emerald-500/30 bg-emerald-500/15 text-emerald-300",
  done: "border-emerald-500/30 bg-emerald-500/15 text-emerald-300",
  incomplete: "border-amber-500/30 bg-amber-500/15 text-amber-300",
  stopped: "border-slate-500/30 bg-slate-500/15 text-slate-300",
  interrupted: "border-orange-500/30 bg-orange-500/15 text-orange-300",
  error: "border-red-500/30 bg-red-500/15 text-red-300",
};

export function History({ onOpen }: { onOpen: (id: string) => void }) {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  async function refresh() {
    setLoading(true);
    try {
      setItems(await listEngagements());
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
  }, []);

  return (
    <div className="mx-auto max-w-4xl p-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <HistoryIcon className="h-4 w-4 text-primary" />
              <CardTitle>Engagement history</CardTitle>
            </div>
            <Button size="sm" variant="outline" onClick={refresh}>
              <RefreshCw className="h-3.5 w-3.5" /> Refresh
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">Every engagement is stored by id and can be reopened with its full details.</p>
        </CardHeader>
        <CardContent className="space-y-2">
          {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
          {!loading && items.length === 0 && <p className="text-sm text-muted-foreground">No engagements yet.</p>}
          {items.map((e) => {
            const p = roadmapProgress(e.roadmap || []);
            return (
              <div key={e.id} className="rounded-md border p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{e.name}</span>
                      <Badge className={statusColor[e.status] || "border-border bg-muted text-muted-foreground"}>{e.status}</Badge>
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-muted-foreground">
                      {e.id} · {new Date(e.created_at).toLocaleString()}
                    </div>
                  </div>
                  <Button size="sm" onClick={() => onOpen(e.id)}>
                    <ExternalLink className="h-3.5 w-3.5" /> Open
                  </Button>
                </div>
                <div className="mt-2 flex items-center gap-3">
                  <Progress value={p.percent} className="flex-1" />
                  <span className="shrink-0 text-xs text-muted-foreground">{p.done}/{p.total} · {e.phase}</span>
                  <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                    <Bug className="h-3.5 w-3.5" /> {e.findings}
                  </span>
                </div>
              </div>
            );
          })}
        </CardContent>
      </Card>
    </div>
  );
}
