import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Button } from "./ui/button";
import { getEngagement } from "../api";
import { NotebookPen, RefreshCw, Download } from "lucide-react";

export function Notes({ engagementId }: { engagementId?: string }) {
  const [notes, setNotes] = useState("");
  const [loading, setLoading] = useState(false);

  async function refresh() {
    if (!engagementId) return;
    setLoading(true);
    try {
      const e: any = await getEngagement(engagementId);
      setNotes(e.notes || "");
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000); // live-ish while the agent works
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [engagementId]);

  function download() {
    const url = URL.createObjectURL(new Blob([notes], { type: "text/markdown" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "engagement-notes.md";
    a.click();
    URL.revokeObjectURL(url);
  }

  if (!engagementId) {
    return <div className="mx-auto max-w-3xl p-6 text-sm text-muted-foreground">Open an engagement to see its working journal.</div>;
  }

  return (
    <div className="mx-auto max-w-4xl p-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <NotebookPen className="h-4 w-4 text-primary" />
              <CardTitle>Working journal (.md)</CardTitle>
            </div>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={refresh}><RefreshCw className="h-3.5 w-3.5" /> Refresh</Button>
              <Button size="sm" variant="outline" onClick={download} disabled={!notes}><Download className="h-3.5 w-3.5" /> .md</Button>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">What the agent tried, what worked/failed, and its next ideas — re-read every step. {loading && "· refreshing"}</p>
        </CardHeader>
        <CardContent>
          {notes ? (
            <pre className="max-h-[32rem] overflow-auto scroll-thin whitespace-pre-wrap break-words rounded-md border bg-black/30 p-3 text-xs leading-relaxed">{notes}</pre>
          ) : (
            <p className="text-xs text-muted-foreground">No journal entries yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
