import { useEffect, useRef } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import type { CommandRun } from "@/types";
import { cn } from "@/lib/utils";
import { TerminalSquare } from "lucide-react";

export function TerminalView({ commands }: { commands: CommandRun[] }) {
  const endRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [commands]);

  return (
    <Card className="flex flex-col">
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <TerminalSquare className="h-4 w-4 text-primary" />
          <CardTitle>Terminal (Terminal MCP)</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <div className="rounded-md bg-black/60 border border-border p-3 font-mono text-xs h-72 overflow-y-auto scroll-thin">
          {commands.length === 0 && <div className="text-muted-foreground">No commands executed yet.</div>}
          {commands.map((c) => (
            <div key={c.cid} className="mb-3">
              <div className="text-emerald-400">
                <span className="text-muted-foreground">$</span> {c.cmdline}
              </div>
              {c.lines.map((l, i) => (
                <div key={i} className={cn(l.stream === "stderr" ? "text-red-400" : "text-slate-300")}>
                  {l.data}
                </div>
              ))}
              {c.done && (
                <div className="text-muted-foreground">
                  [exit {c.exitCode}]
                </div>
              )}
            </div>
          ))}
          <div ref={endRef} />
        </div>
      </CardContent>
    </Card>
  );
}
