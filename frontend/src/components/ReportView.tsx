import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import type { Report } from "@/types";
import { FileText } from "lucide-react";

export function ReportView({ report }: { report: Report }) {
  return (
    <Card className="border-emerald-500/30">
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <FileText className="h-4 w-4 text-emerald-400" />
          <CardTitle>Final Report</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <Section title="Executive summary" body={report.executive_summary} />
        {report.scope && <Section title="Scope" body={report.scope} />}
        {report.methodology && <Section title="Methodology" body={report.methodology} />}
      </CardContent>
    </Card>
  );
}

function Section({ title, body }: { title: string; body: string }) {
  return (
    <div>
      <div className="text-xs font-semibold uppercase text-muted-foreground">{title}</div>
      <p className="mt-1 whitespace-pre-wrap">{body}</p>
    </div>
  );
}
