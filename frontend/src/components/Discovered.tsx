import { useEffect, useMemo, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Button } from "./ui/button";
import { Input } from "./ui/textarea";
import { getEngagement } from "../api";
import { Compass, RefreshCw, Download, Copy, Check } from "lucide-react";

export function Discovered({ engagementId }: { engagementId?: string }) {
  const [endpoints, setEndpoints] = useState<string[]>([]);
  const [links, setLinks] = useState<string[]>([]);
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  async function refresh() {
    if (!engagementId) return;
    setLoading(true);
    try {
      const e: any = await getEngagement(engagementId);
      setEndpoints(e.endpoints || []);
      setLinks(e.links || []);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [engagementId]);

  const fEndpoints = useMemo(() => endpoints.filter((x) => x.toLowerCase().includes(filter.toLowerCase())), [endpoints, filter]);
  const fLinks = useMemo(() => links.filter((x) => x.toLowerCase().includes(filter.toLowerCase())), [links, filter]);

  function download() {
    const data = JSON.stringify({ endpoints, links }, null, 2);
    const url = URL.createObjectURL(new Blob([data], { type: "application/json" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "discovered-urls.json";
    a.click();
    URL.revokeObjectURL(url);
  }
  function copyAll() {
    navigator.clipboard?.writeText([...endpoints, ...links].join("\n"));
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  }

  if (!engagementId) {
    return <div className="mx-auto max-w-4xl p-6 text-sm text-muted-foreground">Open an engagement to see its discovered URLs.</div>;
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4 p-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Compass className="h-4 w-4 text-primary" />
              <CardTitle>Discovered URLs</CardTitle>
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" onClick={refresh}><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
              <Button size="sm" variant="outline" onClick={copyAll}>{copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />} Copy</Button>
              <Button size="sm" variant="outline" onClick={download}><Download className="h-3.5 w-3.5" /> JSON</Button>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            {endpoints.length} API/route endpoints · {links.length} page links {loading && "· loading…"} — saved with the engagement.
          </p>
          <Input className="mt-2" placeholder="filter URLs…" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </CardHeader>
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <UrlList title={`Endpoints (${fEndpoints.length})`} items={fEndpoints} mono />
        <UrlList title={`Links (${fLinks.length})`} items={fLinks} link />
      </div>
    </div>
  );
}

function UrlList({ title, items, mono, link }: { title: string; items: string[]; mono?: boolean; link?: boolean }) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="pb-2"><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent>
        <div className="max-h-[28rem] space-y-0.5 overflow-y-auto scroll-thin rounded-md border bg-black/30 p-2">
          {items.length === 0 && <div className="text-xs text-muted-foreground">none</div>}
          {items.map((u, i) => (
            <div key={i} className={`break-all text-[11px] ${mono ? "font-mono" : ""}`}>
              {link ? <a className="text-primary hover:underline" href={u} target="_blank" rel="noreferrer">{u}</a> : <span className="text-slate-300">{u}</span>}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
