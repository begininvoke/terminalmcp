import * as React from "react";
import { cn } from "@/lib/utils";
import type { Risk } from "@/types";

export function Badge({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors",
        className
      )}
      {...props}
    />
  );
}

const riskStyles: Record<Risk, string> = {
  info: "bg-slate-500/15 text-slate-300 border-slate-500/30",
  low: "bg-emerald-500/15 text-emerald-300 border-emerald-500/30",
  medium: "bg-amber-500/15 text-amber-300 border-amber-500/30",
  high: "bg-orange-500/15 text-orange-300 border-orange-500/30",
  critical: "bg-red-500/20 text-red-300 border-red-500/40",
};

export function RiskBadge({ risk }: { risk: Risk }) {
  return <Badge className={cn("uppercase", riskStyles[risk] ?? riskStyles.info)}>{risk}</Badge>;
}

const statusStyles: Record<string, string> = {
  pending: "bg-slate-500/15 text-slate-400 border-slate-500/30",
  running: "bg-blue-500/15 text-blue-300 border-blue-500/30 animate-pulse",
  done: "bg-emerald-500/15 text-emerald-300 border-emerald-500/30",
  skipped: "bg-slate-500/15 text-slate-400 border-slate-500/30",
  failed: "bg-red-500/15 text-red-300 border-red-500/30",
};

export function StatusBadge({ status }: { status: string }) {
  return <Badge className={cn(statusStyles[status] ?? statusStyles.pending)}>{status}</Badge>;
}
