import { useEffect, useState, type ReactNode } from "react";
import { Button } from "./ui/button";
import { Input } from "./ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Badge } from "./ui/badge";
import { getConfig, updateConfig } from "../api";
import { Loader2, Save, Settings2, CheckCircle2, XCircle } from "lucide-react";

export function Settings() {
  const [cfg, setCfg] = useState<any>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    getConfig().then(setCfg).catch((e) => setErr(String(e)));
  }, []);

  function set(path: string, value: any) {
    setCfg((c: any) => {
      const next = structuredClone(c);
      const parts = path.split(".");
      let o = next;
      for (let i = 0; i < parts.length - 1; i++) o = o[parts[i]];
      o[parts[parts.length - 1]] = value;
      return next;
    });
    setSaved(false);
  }

  async function save() {
    setSaving(true);
    setErr(null);
    try {
      const updated = await updateConfig(cfg);
      setCfg(updated);
      setSaved(true);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  }

  if (err && !cfg) return <Panel><p className="text-sm text-red-400">{err}</p></Panel>;
  if (!cfg) return <Panel><Loader2 className="h-4 w-4 animate-spin" /></Panel>;

  return (
    <div className="mx-auto max-w-3xl space-y-4 p-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center gap-2">
            <Settings2 className="h-4 w-4 text-primary" />
            <CardTitle>LLM provider</CardTitle>
          </div>
          <p className="text-xs text-muted-foreground">
            Changes apply to the next engagement and persist to <code>config.override.yml</code>.
          </p>
        </CardHeader>
        <CardContent className="grid grid-cols-2 gap-3">
          <Field label="Provider">
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
              value={cfg.provider}
              onChange={(e) => set("provider", e.target.value)}
            >
              {["anthropic", "openrouter", "openai", "mock"].map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </Field>
          <Field label="Model"><Input value={cfg.model} onChange={(e) => set("model", e.target.value)} /></Field>
          <Field label="Base URL" full><Input value={cfg.base_url} onChange={(e) => set("base_url", e.target.value)} /></Field>
          <Field label="Tool mode">
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
              value={cfg.tool_mode}
              onChange={(e) => set("tool_mode", e.target.value)}
            >
              {["native", "prompt"].map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </Field>
          <Field label="API key env var"><Input value={cfg.api_key_env} onChange={(e) => set("api_key_env", e.target.value)} /></Field>
          <Field label="API key (set to store in config)" full>
            <Input
              type="password"
              value={cfg.api_key || ""}
              onChange={(e) => set("api_key", e.target.value)}
              placeholder="leave blank to keep current; or paste a key to save it in config"
            />
          </Field>
          <Field label="API key status" full>
            {cfg.api_key_set ? (
              <Badge className="border-emerald-500/30 bg-emerald-500/15 text-emerald-300"><CheckCircle2 className="mr-1 h-3 w-3" /> set in environment</Badge>
            ) : (
              <Badge className="border-red-500/30 bg-red-500/15 text-red-300"><XCircle className="mr-1 h-3 w-3" /> not set — export {cfg.api_key_env}</Badge>
            )}
          </Field>
          <Field label="Temperature"><Input type="number" step="0.1" value={cfg.temperature} onChange={(e) => set("temperature", parseFloat(e.target.value))} /></Field>
          <Field label="Max tokens"><Input type="number" value={cfg.max_tokens} onChange={(e) => set("max_tokens", parseInt(e.target.value))} /></Field>
          <Field label="Max tool iterations"><Input type="number" value={cfg.max_tool_iterations} onChange={(e) => set("max_tool_iterations", parseInt(e.target.value))} /></Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2"><CardTitle>Execution &amp; rate limit</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-2 gap-3">
          <Field label="Command timeout (s)"><Input type="number" value={cfg.command_timeout_s} onChange={(e) => set("command_timeout_s", parseInt(e.target.value))} /></Field>
          <Field label="Output limit (bytes)"><Input type="number" value={cfg.output_limit_bytes} onChange={(e) => set("output_limit_bytes", parseInt(e.target.value))} /></Field>
          <Field label="Rate limit (commands/min, 0 = off)">
            <Input type="number" value={cfg.rate_limit_per_min} onChange={(e) => set("rate_limit_per_min", parseInt(e.target.value))} />
          </Field>
          <Field label="Time budget (min, 0 = no limit)">
            <Input type="number" value={cfg.time_budget_min} onChange={(e) => set("time_budget_min", parseInt(e.target.value))} />
          </Field>
          <Field label="Shell" full><Input value={(cfg.shell || []).join(" ")} onChange={(e) => set("shell", e.target.value.split(" ").filter(Boolean))} /></Field>
          <Field label="Working dir" full><Input value={cfg.workdir} onChange={(e) => set("workdir", e.target.value)} /></Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2"><CardTitle>Chrome MCP (browser testing)</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-2 gap-3">
          <Field label="Enabled">
            <label className="flex h-9 items-center gap-2 text-sm">
              <input type="checkbox" checked={cfg.chrome.enabled} onChange={(e) => set("chrome.enabled", e.target.checked)} />
              use a Chrome MCP server
            </label>
          </Field>
          <Field label="Transport">
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
              value={cfg.chrome.transport}
              onChange={(e) => set("chrome.transport", e.target.value)}
            >
              {["stdio", "sse", "http"].map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </Field>
          <Field label="URL (sse/http)" full><Input value={cfg.chrome.url} onChange={(e) => set("chrome.url", e.target.value)} placeholder="http://localhost:12306/sse" /></Field>
          <Field label="Command (stdio)" full><Input value={(cfg.chrome.command || []).join(" ")} onChange={(e) => set("chrome.command", e.target.value.split(" ").filter(Boolean))} placeholder="npx -y @browsermcp/mcp@latest" /></Field>
        </CardContent>
      </Card>

      <div className="flex items-center justify-end gap-3">
        {err && <span className="text-sm text-red-400">{err}</span>}
        {saved && <span className="text-sm text-emerald-400">Saved ✓</span>}
        <Button onClick={save} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />} Save settings
        </Button>
      </div>
    </div>
  );
}

function Field({ label, children, full }: { label: string; children: ReactNode; full?: boolean }) {
  return (
    <div className={full ? "col-span-2 space-y-1.5" : "space-y-1.5"}>
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
    </div>
  );
}

function Panel({ children }: { children: ReactNode }) {
  return <div className="mx-auto max-w-3xl p-6">{children}</div>;
}
