import { useState, type ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { RiskBadge, Badge } from "./ui/badge";
import { Button } from "./ui/button";
import type { Finding, Risk } from "@/types";
import { Bug, Copy, Download, Check, ChevronDown, ChevronRight, Braces } from "lucide-react";

const order: Risk[] = ["critical", "high", "medium", "low", "info"];

const confidenceStyle: Record<string, string> = {
  confirmed: "border-emerald-500/30 bg-emerald-500/15 text-emerald-300",
  suspected: "border-amber-500/30 bg-amber-500/15 text-amber-300",
  false_positive: "border-slate-500/30 bg-slate-500/15 text-slate-400",
};

function download(name: string, data: string) {
  const blob = new Blob([data], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

export function Findings({ findings }: { findings: Finding[] }) {
  const counts = order.map((r) => ({ r, n: findings.filter((f) => f.severity === r).length }));

  return (
    <Card className="h-full flex flex-col">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bug className="h-4 w-4 text-primary" />
            <CardTitle>Findings &amp; Risk</CardTitle>
          </div>
          {findings.length > 0 && (
            <Button size="sm" variant="outline" onClick={() => download("findings.json", JSON.stringify(findings, null, 2))}>
              <Download className="h-3.5 w-3.5" /> JSON
            </Button>
          )}
        </div>
        <div className="flex flex-wrap gap-1.5 pt-1">
          {counts.map(({ r, n }) => (
            <div key={r} className="flex items-center gap-1">
              <RiskBadge risk={r} />
              <span className="text-xs text-muted-foreground">{n}</span>
            </div>
          ))}
        </div>
      </CardHeader>
      <CardContent className="flex-1 overflow-y-auto scroll-thin space-y-2">
        {findings.length === 0 && <p className="text-xs text-muted-foreground">No findings yet.</p>}
        {findings.map((f) => (
          <FindingCard key={f.id} f={f} />
        ))}
      </CardContent>
    </Card>
  );
}

function FindingCard({ f }: { f: Finding }) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  function copyJSON() {
    navigator.clipboard?.writeText(JSON.stringify(f, null, 2));
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  }

  return (
    <div className="rounded-md border p-2.5">
      <div className="flex items-start justify-between gap-2">
        <button onClick={() => setOpen((o) => !o)} className="flex min-w-0 items-start gap-1.5 text-left">
          {open ? <ChevronDown className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
          <span className="text-sm font-medium">{f.title}</span>
        </button>
        <RiskBadge risk={f.severity} />
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-1.5">
        {f.type && <Badge className="border-border bg-muted text-muted-foreground">{f.type}</Badge>}
        {f.confidence && <Badge className={confidenceStyle[f.confidence] || ""}>{f.confidence}</Badge>}
        {f.cwe && <Badge className="border-border bg-muted text-muted-foreground">{f.cwe}</Badge>}
        {f.owasp && <Badge className="border-border bg-muted text-muted-foreground">{f.owasp}</Badge>}
      </div>

      {f.endpoint && <div className="mt-1.5 break-all font-mono text-[11px] text-muted-foreground">{f.parameter ? `${f.parameter} @ ` : ""}{f.endpoint}</div>}

      {open && (
        <div className="mt-2 space-y-2 text-xs">
          {f.description && <Field label="Description" value={f.description} />}
          {f.impact && <Field label="Impact" value={f.impact} />}
          {f.cvss && <Field label="CVSS" value={f.cvss} mono />}
          {f.steps_to_reproduce && f.steps_to_reproduce.length > 0 && (
            <div>
              <Label>Steps to reproduce</Label>
              <ol className="ml-4 list-decimal space-y-0.5">{f.steps_to_reproduce.map((s, i) => <li key={i}>{s}</li>)}</ol>
            </div>
          )}
          {f.request && <Pre label="Request" value={f.request} />}
          {f.response && <Pre label="Response" value={f.response} />}
          {f.evidence && <Field label="Evidence" value={f.evidence} />}
          {f.remediation && <Field label="Remediation" value={f.remediation} />}
          {f.references && f.references.length > 0 && (
            <div>
              <Label>References</Label>
              <ul className="ml-4 list-disc space-y-0.5">
                {f.references.map((r, i) => (
                  <li key={i}><a className="text-primary underline break-all" href={r} target="_blank" rel="noreferrer">{r}</a></li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      <div className="mt-2 flex justify-end">
        <Button size="sm" variant="ghost" onClick={copyJSON} className="h-7 text-xs">
          {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Braces className="h-3.5 w-3.5" />} {copied ? "Copied" : "Copy JSON"}
        </Button>
      </div>
    </div>
  );
}

function Label({ children }: { children: ReactNode }) {
  return <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{children}</div>;
}
function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <Label>{label}</Label>
      <div className={mono ? "font-mono text-[11px] break-all" : ""}>{value}</div>
    </div>
  );
}
function Pre({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Label>{label}</Label>
      <pre className="mt-0.5 max-h-40 overflow-auto scroll-thin whitespace-pre-wrap break-words rounded bg-black/50 p-2 font-mono text-[11px]">{value}</pre>
    </div>
  );
}
