import type { ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { RiskBadge } from "./ui/badge";
import type { Risk, NextAction } from "@/types";
import { Activity, Bot, Play, Lightbulb, Microscope } from "lucide-react";

export type TimelineEntry =
  | { kind: "message"; text: string }
  | { kind: "step"; stepId: string; title: string; rationale?: string }
  | { kind: "analysis"; stepId: string; summary: string; risk: Risk }
  | { kind: "recommendation"; stepId: string; actions: NextAction[] };

export function ActivityFeed({ entries }: { entries: TimelineEntry[] }) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <Activity className="h-4 w-4 text-primary" />
          <CardTitle>Agent activity &amp; analysis</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-2 max-h-80 overflow-y-auto scroll-thin">
        {entries.length === 0 && <p className="text-xs text-muted-foreground">Waiting for the agent…</p>}
        {entries.map((e, i) => {
          if (e.kind === "message")
            return (
              <Row key={i} icon={<Bot className="h-4 w-4 text-blue-400" />}>
                <span className="text-sm text-muted-foreground">{e.text}</span>
              </Row>
            );
          if (e.kind === "step")
            return (
              <Row key={i} icon={<Play className="h-4 w-4 text-primary" />}>
                <div className="text-sm font-medium">{e.title}</div>
                {e.rationale && <div className="text-xs text-muted-foreground">{e.rationale}</div>}
              </Row>
            );
          if (e.kind === "analysis")
            return (
              <Row key={i} icon={<Microscope className="h-4 w-4 text-amber-400" />}>
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold uppercase text-muted-foreground">Analysis</span>
                  <RiskBadge risk={e.risk} />
                </div>
                <div className="text-sm">{e.summary}</div>
              </Row>
            );
          return (
            <Row key={i} icon={<Lightbulb className="h-4 w-4 text-emerald-400" />}>
              <div className="text-xs font-semibold uppercase text-muted-foreground">Recommended next</div>
              <ul className="mt-0.5 space-y-0.5">
                {e.actions.map((a, j) => (
                  <li key={j} className="text-sm">
                    <span className="font-medium">{a.title}</span>
                    {a.why && <span className="text-muted-foreground"> — {a.why}</span>}
                  </li>
                ))}
              </ul>
            </Row>
          );
        })}
      </CardContent>
    </Card>
  );
}

function Row({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex gap-2.5 rounded-md border p-2.5">
      <div className="mt-0.5 shrink-0">{icon}</div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
