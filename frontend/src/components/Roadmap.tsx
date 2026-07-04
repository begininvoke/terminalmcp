import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { StatusBadge } from "./ui/badge";
import type { RoadmapStep } from "@/types";
import { cn } from "@/lib/utils";
import { Map } from "lucide-react";

export function Roadmap({ steps, activeStepId }: { steps: RoadmapStep[]; activeStepId?: string }) {
  return (
    <Card className="h-full flex flex-col">
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <Map className="h-4 w-4 text-primary" />
          <CardTitle>Roadmap</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="flex-1 overflow-y-auto scroll-thin space-y-2">
        {steps.length === 0 && (
          <p className="text-xs text-muted-foreground">Waiting for the agent to build the plan…</p>
        )}
        {steps.map((s) => (
          <div
            key={s.id}
            className={cn(
              "rounded-md border p-2.5 transition-colors",
              activeStepId === s.id ? "border-primary/60 bg-primary/5" : "border-border"
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{s.phase}</span>
              <StatusBadge status={s.status} />
            </div>
            <div className="mt-1 text-sm font-medium">{s.title}</div>
            {s.intent && <div className="mt-0.5 text-xs text-muted-foreground">{s.intent}</div>}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
