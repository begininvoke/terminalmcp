import { useState } from "react";
import { Button } from "./ui/button";
import { Textarea, Input } from "./ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Terminal, ShieldAlert, Loader2 } from "lucide-react";

export const DEFAULT_PROMPT = `I want to run a black-box penetration test.
First, analyze the target and ask for any required details.
Then create a clear step-by-step testing roadmap.
Use Terminal MCP as the main execution layer. Do not rely on separate MCP tools like Nmap MCP, Curl MCP, or other command-specific MCP wrappers.
You may run any required command through the terminal MCP, read the output, analyze it, and decide the next step.
Show the roadmap and each step in the UI.
After each step, show the results, findings, risk level, and recommended next steps.`;

export function PromptEntry({
  onStart,
  starting,
}: {
  onStart: (name: string, prompt: string, goal: string, cookie: string, squad: string[], target: string) => void;
  starting: boolean;
}) {
  const [name, setName] = useState("");
  const [target, setTarget] = useState("");
  const [goal, setGoal] = useState("");
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [cookie, setCookie] = useState("");
  const [squad, setSquad] = useState(false);
  const SQUAD_CLASSES = ["IDOR", "XSS", "SQLi", "Broken Access Control", "SSRF"];

  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <Card className="w-full max-w-3xl">
        <CardHeader>
          <div className="flex items-center gap-2">
            <ShieldAlert className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">AI Black-Box Penetration Test</CardTitle>
          </div>
          <p className="text-sm text-muted-foreground">
            Write or edit the first prompt below, then start the engagement. The agent will analyze the
            target, ask for required details, build a roadmap, and execute tools through the Terminal MCP.
          </p>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Target URL <span className="text-red-400">*</span></label>
            <Input
              placeholder="https://www.digikalajet.com/"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">The exact in-scope target. The agent uses this host in every command — no placeholders.</p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">Engagement name (optional)</label>
              <Input
                placeholder="e.g. acme.com external assessment"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">Goal (optional — focus the agent)</label>
              <Input
                placeholder="e.g. find IDOR"
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">First prompt</label>
            <Textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={10}
              className="font-mono text-xs leading-relaxed"
            />
            <p className="text-xs text-muted-foreground">
              This is the exact instruction the agent receives. Edit it freely — add your target, scope, or
              constraints.
            </p>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Session cookie (optional — for authenticated discovery / IDOR)</label>
            <Textarea
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              rows={2}
              className="font-mono text-xs"
              placeholder="e.g. sessionid=abc123; csrftoken=… — paste a logged-in account's Cookie header so browser_capture & idor_scan run authenticated"
            />
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={squad} onChange={(e) => setSquad(e.target.checked)} />
            <span className="font-medium">Multi-agent squad</span>
            <span className="text-xs text-muted-foreground">— run {SQUAD_CLASSES.length} parallel specialists ({SQUAD_CLASSES.join(", ")}) instead of one agent</span>
          </label>

          <div className="flex justify-end">
            <Button onClick={() => onStart(name, prompt, goal, cookie, squad ? SQUAD_CLASSES : [], target)} disabled={starting || !prompt.trim() || !target.trim()}>
              {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Terminal className="h-4 w-4" />}
              Start engagement
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
