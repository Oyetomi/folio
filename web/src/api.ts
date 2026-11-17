// Typed client for the Go server in internal/server. Shapes mirror the Go
// structs sourceInfo, jobJSON and resultJSON.

export type SourceKind = "folder" | "epub" | "github" | "path";

export interface Env {
  version: string;
  tectonic: boolean;
  pdftotext: boolean;
  git: boolean;
  proFonts: boolean;
}

export type FontMode = "open" | "pro" | "mono";

export interface ChapterInfo {
  title: string;
  part: number; // 0 = front matter, 999+ = appendix
}

export interface PartInfo {
  index: number;
  title: string;
}

export interface SourceInfo {
  id: string;
  kind: SourceKind;
  label: string;
  title: string;
  subtitle: string;
  author: string;
  noIndex: boolean;
  fonts: string;
  chapters: ChapterInfo[] | null;
  parts: PartInfo[] | null;
}

export interface BuildResult {
  pages: number;
  chapters: number;
  figures: number;
  figuresAudited: number;
  overlaps: number;
  overfull: number;
  verified: boolean;
  sizeBytes: number;
  missingGlyphs: number;
  missingSample: string;
  fonts: string;
  seconds: number;
  subtitle: string;
}

export type JobState = "queued" | "running" | "done" | "failed" | "cancelled";

export interface Job {
  id: string;
  label: string;
  title: string;
  author: string;
  state: JobState;
  stage: string;
  created: string;
  error?: string;
  result?: BuildResult;
}

export interface BuildRequest {
  sourceId: string;
  title: string;
  subtitle: string;
  author: string;
  noIndex: boolean;
  verify: boolean;
  fonts: FontMode;
}

export const isActive = (j: Pick<Job, "state">) => j.state === "queued" || j.state === "running";

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = res.status === 204 ? null : await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data && (data as { error?: string }).error) || `The desk answered ${res.status}.`);
  return data as T;
}

export const api = {
  env: () => request<Env>("GET", "/api/env"),
  builds: () => request<Job[]>("GET", "/api/builds"),
  build: (id: string) => request<Job>("GET", `/api/builds/${id}`),
  startBuild: (req: BuildRequest) => request<Job>("POST", "/api/builds", req),
  cancelBuild: (id: string) => request<null>("DELETE", `/api/builds/${id}`),
};

export const artifactUrl = (id: string, kind: "pdf" | "tex", download = false) =>
  `/api/builds/${id}/${kind}${download ? "?download=1" : ""}`;

/**
 * Upload a manuscript source. Uses XMLHttpRequest because fetch() cannot
 * report upload progress. onProgress receives 0–100, or null once the bytes
 * are sent and the server is inspecting the manuscript.
 */
export function createSource(fd: FormData, onProgress: (pct: number | null) => void): Promise<SourceInfo> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/sources");
    xhr.responseType = "json";
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return;
      const pct = (e.loaded / e.total) * 100;
      onProgress(pct >= 99.5 ? null : pct);
    };
    xhr.onload = () => {
      const body = (xhr.response || {}) as SourceInfo & { error?: string };
      if (xhr.status >= 200 && xhr.status < 300) resolve(body);
      else reject(new Error(body.error || `The desk answered ${xhr.status}.`));
    };
    xhr.onerror = () => reject(new Error("Lost contact with the Folio desk. Is `folio serve` still running?"));
    xhr.send(fd);
  });
}
