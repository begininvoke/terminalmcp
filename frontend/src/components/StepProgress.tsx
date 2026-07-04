import { Card, CardContent } from "./ui/card";
import { Progress } from "./ui/progress";
import type { RoadmapStep } from "@/types";
import { roadmapProgress } from "@/lib/utils";
import { CheckCircle2, Loader2, Circle, XCircle } from "lucide-react";

export function StepProgress({ roadmap, phase }: { roadmap: RoadmapStep[]; phase: string }) {
  const p = roadmapProgress(roadmap);
  return (
    <Card>
      <CardContent className="space-y-2 p-4">
        <div className="flex items-center justify-between text-sm">
          <span className="font-medium">Progress</span>
          <span className="text-muted-foreground">
            {p.done}/{p.total} steps · {p.percent}% · phase: <span className="text-foreground">{phase}</span>
          </span>
        </div>
        <Progress value={p.percent} />
        <div className="flex flex-wrap gap-3 pt-1 text-xs text-muted-foreground">
          <span className="flex items-center gap-1"><CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" /> {p.done} done</span>
          <span className="flex items-center gap-1"><Loader2 className="h-3.5 w-3.5 text-blue-400" /> {p.running} running</span>
          <span className="flex items-center gap-1"><Circle className="h-3.5 w-3.5 text-slate-400" /> {p.total - p.done - p.running - p.failed} pending</span>
          {p.failed > 0 && <span className="flex items-center gap-1"><XCircle className="h-3.5 w-3.5 text-red-400" /> {p.failed} failed</span>}
        </div>
      </CardContent>
    </Card>
  );
}
