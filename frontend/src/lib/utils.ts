import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { RoadmapStep } from "@/types";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function roadmapProgress(steps: RoadmapStep[]) {
  const total = steps.length;
  const done = steps.filter((s) => s.status === "done" || s.status === "skipped").length;
  const running = steps.filter((s) => s.status === "running").length;
  const failed = steps.filter((s) => s.status === "failed").length;
  const percent = total ? Math.round((done / total) * 100) : 0;
  return { total, done, running, failed, percent };
}
