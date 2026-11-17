import { useEffect, useState } from "react";
import type { Job } from "../api";

export const STAGES = [
  { id: "queued", name: "Queued" },
  { id: "fetch", name: "Cloning repository" },
  { id: "discover", name: "Reading chapters" },
  { id: "typeset", name: "Generating LaTeX and diagrams" },
  { id: "compile", name: "Compiling PDF" },
  { id: "verify", name: "Checking figures" },
] as const;

const STAGE_PROGRESS: Record<string, number> = {
  queued: 0.04, fetch: 0.1, discover: 0.18, typeset: 0.3, compile: 0.42, verify: 0.9,
};

export type StreamStatus = "idle" | "streaming" | "done" | "failed" | "cancelled" | "lost";

export interface BuildStream {
  status: StreamStatus;
  stage: string | null;
  seen: string[];
  lines: string[];
  progress: number; // 0–1, monotonic per build
  result: Job | null;
  message: string;
}

const initial: BuildStream = { status: "idle", stage: null, seen: [], lines: [], progress: 0, result: null, message: "" };

/** Follows a build's Server-Sent Events: stages, log lines and the outcome. */
export function useBuildStream(jobId: string | null): BuildStream {
  const [s, setS] = useState<BuildStream>(initial);

  useEffect(() => {
    if (!jobId) {
      setS(initial);
      return;
    }
    setS({ ...initial, status: "streaming", progress: 0.02 });

    const es = new EventSource(`/api/builds/${jobId}/events`);
    let progress = 0.02;
    let pending: string[] = [];
    let raf = 0;
    let opens = 0;
    const bump = (p: number) => { progress = Math.max(progress, p); };
    const parse = (e: MessageEvent) => {
      try { return JSON.parse(e.data) as Record<string, unknown>; } catch { return {}; }
    };
    // Batch log lines into one render per frame; Tectonic can be chatty.
    const flush = () => {
      raf = 0;
      if (!pending.length) return;
      const add = pending;
      pending = [];
      setS((x) => ({ ...x, lines: x.lines.concat(add), progress }));
    };
    const end = (patch: Partial<BuildStream>) => {
      es.close();
      cancelAnimationFrame(raf);
      flush();
      setS((x) => ({ ...x, ...patch }));
    };

    es.onopen = () => {
      // The server replays the full history on reconnect; start over.
      if (opens++ > 0) {
        pending = [];
        setS((x) => ({ ...x, lines: [], seen: [] }));
      }
    };
    es.addEventListener("stage", (e) => {
      const stage = String(parse(e as MessageEvent).stage ?? "");
      bump(STAGE_PROGRESS[stage] ?? 0);
      setS((x) => ({ ...x, stage, seen: x.seen.includes(stage) ? x.seen : [...x.seen, stage], progress }));
    });
    es.addEventListener("log", (e) => {
      const line = String(parse(e as MessageEvent).line ?? "");
      // Tectonic narrates its passes; nudge the ribbon through compilation.
      if (/Rerunning TeX/i.test(line)) bump(Math.min(0.8, progress + 0.1));
      else if (/Running TeX/i.test(line)) bump(0.55);
      else if (/xdvipdfmx/i.test(line)) bump(0.86);
      pending.push(line);
      if (!raf) raf = requestAnimationFrame(flush);
    });
    es.addEventListener("done", (e) => end({ status: "done", progress: 1, result: parse(e as MessageEvent) as unknown as Job }));
    es.addEventListener("failed", (e) => end({ status: "failed", message: String(parse(e as MessageEvent).message ?? "Build failed.") }));
    es.addEventListener("cancelled", () => end({ status: "cancelled", message: "Build cancelled." }));
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) end({ status: "lost", message: "Lost connection to the Folio server." });
    };

    return () => {
      es.close();
      cancelAnimationFrame(raf);
    };
  }, [jobId]);

  return s;
}
