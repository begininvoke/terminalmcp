import { useState } from "react";
import { Button } from "./ui/button";
import { Textarea } from "./ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { MessageCircleQuestion, Send } from "lucide-react";

export function IntakeDialog({
  prompt,
  questions,
  options,
  onSubmit,
}: {
  prompt?: string;
  questions: string[];
  options?: string[];
  onSubmit: (text: string) => void;
}) {
  const [text, setText] = useState("");
  const isDecision = options && options.length > 0;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-6">
      <Card className={`w-full max-w-xl ${isDecision ? "border-red-500/40" : "border-primary/40"}`}>
        <CardHeader className="pb-2">
          <div className="flex items-center gap-2">
            <MessageCircleQuestion className={`h-5 w-5 ${isDecision ? "text-red-400" : "text-primary"}`} />
            <CardTitle>{isDecision ? "A job failed — how should the agent proceed?" : "The agent needs details before testing"}</CardTitle>
          </div>
          {prompt && <p className="text-sm text-muted-foreground break-words">{prompt}</p>}
        </CardHeader>
        <CardContent className="space-y-3">
          <ul className="list-disc space-y-1 pl-5 text-sm">
            {questions.map((q, i) => (
              <li key={i}>{q}</li>
            ))}
          </ul>

          {isDecision ? (
            <div className="flex justify-end gap-2">
              {options!.map((opt) => (
                <Button key={opt} variant={opt === "retry" ? "default" : "secondary"} onClick={() => onSubmit(opt)}>
                  {opt}
                </Button>
              ))}
            </div>
          ) : (
            <>
              <Textarea
                autoFocus
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="Answer the questions here (scope, authorization, constraints, credentials…)"
                rows={5}
              />
              <div className="flex justify-end">
                <Button onClick={() => onSubmit(text)} disabled={!text.trim()}>
                  <Send className="h-4 w-4" /> Send answers
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
