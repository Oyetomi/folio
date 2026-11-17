import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { api, createSource, isActive, type Env, type FontMode, type Job, type SourceInfo, type SourceKind } from "./api";
import { TitlePage, type TitlePageValues } from "./components/TitlePage";
import { Manifest } from "./components/Manifest";
import { Masthead } from "./components/Masthead";
import { Press } from "./components/Press";
import { ProofView } from "./components/ProofView";
import { Shelf } from "./components/Shelf";
import { SourcePicker } from "./components/SourcePicker";
import { Specimen } from "./components/Specimen";
import { useBuildStream } from "./hooks/useBuildStream";
import { useTheme } from "./hooks/useTheme";
import { fmtBytes, isEpub, keepFile, readEntry, type PathedFile } from "./lib/files";

type Mode = "idle" | "press" | "proof" | "error";

const ERROR_TITLE: Record<string, string> = {
  failed: "Build failed",
  cancelled: "Build cancelled",
  lost: "Connection lost",
};

const ERROR_LEDE: Record<string, string> = {
  failed: "The error is below. The full log has more context.",
  cancelled: "Nothing was written. Press Retry to start again.",
  lost: "The page can't reach the Folio server. Check that `folio serve` is still running.",
};

const blankForm: TitlePageValues = { title: "", subtitle: "", author: "", index: true, verify: true, fonts: "open" };

const asFontMode = (s: string | undefined): FontMode => (s === "pro" || s === "mono" ? s : "open");

export function App() {
  const toggleTheme = useTheme();
  const [env, setEnv] = useState<Env | null>(null);

  // Verso: the manuscript.
  const [kind, setKind] = useState<SourceKind>("folder");
  const [source, setSource] = useState<SourceInfo | null>(null);
  const [upload, setUpload] = useState<{ label: string; pct: number | null } | null>(null);
  const [sourceError, setSourceError] = useState("");
  const [form, setForm] = useState<TitlePageValues>(blankForm);

  // Recto: the proof.
  const [mode, setMode] = useState<Mode>("idle");
  const [current, setCurrent] = useState<Job | null>(null);
  const [streamId, setStreamId] = useState<string | null>(null);
  const [startedAt, setStartedAt] = useState(0);
  const stream = useBuildStream(streamId);
  const rectoRef = useRef<HTMLElement>(null);

  // The shelf.
  const [jobs, setJobs] = useState<Job[]>([]);
  const refreshShelf = useCallback(async () => {
    try { setJobs(await api.builds()); } catch { /* desk gone */ }
  }, []);

  useEffect(() => {
    api.env().then(setEnv).catch(() => {});
    refreshShelf();
  }, [refreshShelf]);

  // React to the end of a build.
  useEffect(() => {
    if (stream.status === "done" && stream.result) {
      setCurrent(stream.result);
      setMode("proof");
      refreshShelf();
    } else if (stream.status === "failed" || stream.status === "cancelled" || stream.status === "lost") {
      setMode("error");
      refreshShelf();
    }
  }, [stream.status, stream.result, refreshShelf]);

  // ── loading a manuscript ────────────────────────────────
  const applySource = useCallback((info: SourceInfo | null) => {
    setSource(info);
    setForm({
      title: info?.title ?? "",
      subtitle: info?.subtitle ?? "",
      author: info?.author ?? "",
      index: !(info?.noIndex ?? false),
      verify: true,
      fonts: asFontMode(info?.fonts),
    });
  }, []);

  const submitSource = useCallback((fd: FormData, label: string) => {
    setSourceError("");
    setUpload({ label, pct: fd.has("files") || fd.has("file") ? 0 : null });
    createSource(fd, (pct) => setUpload({ label: pct == null ? "Reading files…" : label, pct }))
      .then(applySource)
      .catch((e: Error) => setSourceError(e.message))
      .finally(() => setUpload(null));
  }, [applySource]);

  const onFolder = useCallback((entries: PathedFile[]) => {
    const kept = entries.filter((e) => keepFile(e.path, e.file.size));
    if (!kept.length) {
      setSourceError("No Markdown, notebook or image files in that folder.");
      return;
    }
    const fd = new FormData();
    fd.append("kind", "folder");
    let bytes = 0;
    for (const { file, path } of kept) {
      fd.append("paths", path);
      fd.append("files", file, file.name);
      bytes += file.size;
    }
    submitSource(fd, `Uploading ${kept.length} files · ${fmtBytes(bytes)}`);
  }, [submitSource]);

  const onEpub = useCallback((file: File) => {
    if (!isEpub(file.name)) {
      setSourceError("That isn't an .epub file.");
      return;
    }
    const fd = new FormData();
    fd.append("kind", "epub");
    fd.append("file", file, file.name);
    submitSource(fd, `Uploading ${file.name} · ${fmtBytes(file.size)}`);
  }, [submitSource]);

  const onGitHub = useCallback((url: string) => {
    const fd = new FormData();
    fd.append("kind", "github");
    fd.append("url", url);
    submitSource(fd, "Cloning " + url.replace(/^https?:\/\//, ""));
  }, [submitSource]);

  const onPath = useCallback((path: string) => {
    const fd = new FormData();
    fd.append("kind", "path");
    fd.append("path", path);
    submitSource(fd, "Opening " + path);
  }, [submitSource]);

  // Drop anywhere in the window.
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    document.body.classList.toggle("dragging", dragging);
  }, [dragging]);
  useEffect(() => {
    let depth = 0;
    const hasFiles = (e: DragEvent) => [...(e.dataTransfer?.types ?? [])].includes("Files");
    const enter = (e: DragEvent) => { if (hasFiles(e)) { depth++; setDragging(true); } };
    const leave = () => { depth = Math.max(0, depth - 1); if (!depth) setDragging(false); };
    const over = (e: DragEvent) => { if (hasFiles(e)) e.preventDefault(); };
    const drop = async (e: DragEvent) => {
      if (!hasFiles(e) || !e.dataTransfer) return;
      e.preventDefault();
      depth = 0;
      setDragging(false);
      const dt = e.dataTransfer;
      const entries = [...dt.items].map((i) => i.webkitGetAsEntry?.()).filter((x): x is FileSystemEntry => !!x);
      const files = [...dt.files];
      if (entries.length === 1 && entries[0].isFile && isEpub(entries[0].name)) {
        setKind("epub");
        onEpub(files[0]);
        return;
      }
      if (entries.length) {
        setKind("folder");
        setUpload({ label: "Reading the folder…", pct: null });
        const out: PathedFile[] = [];
        try {
          for (const it of entries) await readEntry(it, "", out);
        } catch {
          setUpload(null);
          setSourceError("The browser couldn't read that folder.");
          return;
        }
        setUpload(null);
        onFolder(out);
        return;
      }
      if (files[0] && isEpub(files[0].name)) onEpub(files[0]);
    };
    addEventListener("dragenter", enter);
    addEventListener("dragleave", leave);
    addEventListener("dragover", over);
    addEventListener("drop", drop);
    return () => {
      removeEventListener("dragenter", enter);
      removeEventListener("dragleave", leave);
      removeEventListener("dragover", over);
      removeEventListener("drop", drop);
    };
  }, [onEpub, onFolder]);

  // ── building ────────────────────────────────────────────
  const pressing = mode === "press" && stream.status === "streaming";
  const canBuild = !!source && (!env || env.tectonic) && !pressing;
  const disabledReason = env && !env.tectonic ? "Install Tectonic to build PDFs." : !source ? "Load a source first." : "";
  const [lastBuiltSource, setLastBuiltSource] = useState<string | null>(null);

  // Show progress in the browser tab so the build can be followed from elsewhere.
  useEffect(() => {
    const base = "Folio Desk";
    if (mode === "press" && stream.status === "streaming") {
      document.title = `${Math.round(stream.progress * 100)}% · Building ${current?.title || "book"} · ${base}`;
    } else if (mode === "proof" && current) {
      document.title = `✓ ${current.title || "Book"} · ${base}`;
    } else if (mode === "error") {
      document.title = `Build failed · ${base}`;
    } else {
      document.title = base;
    }
  }, [mode, stream.status, stream.progress, current]);

  // Bring the preview into view when its top is off-screen, e.g. after
  // pressing Build at the bottom of the left page.
  const revealPreview = useCallback(() => {
    requestAnimationFrame(() => {
      const el = rectoRef.current;
      if (!el) return;
      const top = el.getBoundingClientRect().top;
      if (top >= 0 && top < innerHeight * 0.35) return;
      const smooth = !matchMedia("(prefers-reduced-motion: reduce)").matches;
      scrollTo({ top: Math.max(0, scrollY + top - 16), behavior: smooth ? "smooth" : "auto" });
    });
  }, []);

  const beginPress = useCallback((job: Job) => {
    setCurrent(job);
    setStartedAt(performance.now() - Math.max(0, Date.now() - Date.parse(job.created)));
    setStreamId(job.id);
    setMode("press");
    refreshShelf();
    revealPreview();
  }, [refreshShelf, revealPreview]);

  const startBuild = useCallback(async () => {
    if (!canBuild || !source) return;
    try {
      const job = await api.startBuild({
        sourceId: source.id,
        title: form.title.trim(),
        subtitle: form.subtitle.trim(),
        author: form.author.trim(),
        noIndex: !form.index,
        verify: !!env?.pdftotext && form.verify,
        fonts: form.fonts === "pro" && !env?.proFonts ? "open" : form.fonts,
      });
      setLastBuiltSource(source.id);
      beginPress(job);
    } catch (e) {
      setSourceError((e as Error).message);
    }
  }, [canBuild, source, form, env, beginPress]);

  // ⌘/Ctrl + Enter anywhere sets the book in type.
  const startRef = useRef(startBuild);
  startRef.current = startBuild;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        startRef.current();
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);

  const openFromShelf = useCallback(async (id: string) => {
    let job: Job;
    try { job = await api.build(id); } catch { return; }
    if (isActive(job)) {
      beginPress(job);
    } else if (job.state === "done") {
      setStreamId(null);
      setCurrent(job);
      setMode("proof");
      revealPreview();
    }
  }, [beginPress, revealPreview]);

  const progress = mode === "proof" ? 1 : mode === "press" ? stream.progress : 0;
  const ribbonStyle = { "--p": progress } as CSSProperties;

  return (
    <>
      <a className="skip" href="#desk">Skip to the desk</a>
      <Masthead onToggleTheme={toggleTheme} />

      <main className="spread" id="desk">
        <section className="page verso rise d2" aria-labelledby="ms-title">
          <div className="running-head">
            <span>Source</span>
            <span>{source ? source.title || source.label : "Nothing loaded"}</span>
          </div>
          <h1 className="display" id="ms-title">Make a book</h1>
          <p className="lede">
            Load a folder of Markdown, Jupyter notebooks, an EPUB or a GitHub repository. Folio typesets it with XeLaTeX and
            shows you the finished PDF.
          </p>

          {env && !env.tectonic && (
            <p className="notice notice-warn setup-notice">
              <strong>Tectonic is not installed.</strong> Folio needs it to compile PDFs. Run <code>brew install tectonic</code> (or see
              tectonic-typesetting.github.io), then reload this page.
            </p>
          )}

          <div className={"step" + (source ? " done" : "")}>
            <span className="step-num">
              {source ? <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 8.4l2.6 2.5L12 5.4" /></svg> : "1"}
            </span>
            <span className="step-title">Source</span>
            {source && <span className="step-note">Loaded</span>}
          </div>

          {!source && <SourcePicker
            kind={kind}
            onKind={setKind}
            onFolder={onFolder}
            onEpub={onEpub}
            onGitHub={onGitHub}
            onPath={onPath}
            gitAvailable={env ? env.git : true}
          />}

          {upload && (
            <div className="upload">
              <div className="upload-row">
                <span>{upload.label}</span>
                <span className="num">{upload.pct == null ? "" : `${Math.round(upload.pct)}%`}</span>
              </div>
              <div className={"meter" + (upload.pct == null ? " indeterminate" : "")}>
                <span style={upload.pct == null ? undefined : { width: `${upload.pct}%` }} />
              </div>
            </div>
          )}
          {sourceError && <p className="notice notice-error" role="alert">{sourceError}</p>}

          {source && <Manifest key={source.id} info={source} onClear={() => applySource(null)} />}

          <TitlePage
            values={form}
            onChange={setForm}
            onSubmit={startBuild}
            canBuild={canBuild}
            auditAvailable={env ? env.pdftotext : true}
            proAvailable={!!env?.proFonts}
            busy={pressing}
            disabledReason={disabledReason}
            rebuild={!!source && source.id === lastBuiltSource}
          />
          <div className="folio-num" aria-hidden="true">i</div>
        </section>

        <div className="gutter" aria-hidden="true" />

        <section className="page recto rise d3" aria-labelledby="proof-title" aria-live="polite" ref={rectoRef}>
          <div className="running-head">
            <span>Preview</span>
            <span>{mode !== "idle" && current?.title ? current.title : ""}</span>
          </div>
          <div className={"ribbon" + (progress > 0 ? " on" : "") + (mode === "proof" || (mode === "press" && stream.status === "done") ? " done" : "")} style={ribbonStyle} aria-hidden="true" />

          {mode === "idle" && (
            <div className="state" id="st-idle">
              <h2 className="h-serif" id="proof-title">No PDF yet</h2>
              <p className="muted">Load a source and press Build PDF. Progress and the finished book appear here.</p>
              <div className="tray">
                <Specimen />
              </div>
            </div>
          )}

          {mode === "press" && current && (
            <Press
              key={current.id}
              title={current.title}
              author={current.author}
              stream={stream}
              startedAt={startedAt}
              onStop={() => { api.cancelBuild(current.id).catch(() => {}); }}
              onBack={stream.status === "done" ? () => setMode("proof") : undefined}
            />
          )}

          {mode === "proof" && current && (
            <ProofView key={current.id} job={current} onShowLog={streamId === current.id && stream.lines.length ? () => setMode("press") : undefined} />
          )}

          {mode === "error" && (
            <div className="state">
              <h2 className="h-serif">{ERROR_TITLE[stream.status] ?? ERROR_TITLE.failed}</h2>
              <p className="muted">{ERROR_LEDE[stream.status] ?? ERROR_LEDE.failed}</p>
              <pre className="erratum" tabIndex={0}>{stream.message}</pre>
              <div className="actions">
                <button className="btn btn-press btn-md" type="button" onClick={startBuild} disabled={!canBuild}>Retry</button>
                <button className="btn btn-ghost btn-md" type="button" onClick={() => setMode("press")}>View log</button>
              </div>
            </div>
          )}

          <div className="folio-num" aria-hidden="true">ii</div>
        </section>
      </main>

      <Shelf jobs={jobs} activeId={mode === "proof" ? current?.id ?? null : null} onOpen={openFromShelf} />

      <footer className="site-foot">
        <p>Folio {env ? `v${env.version}` : ""} · running on this computer</p>
      </footer>

      <div className="drop-veil" aria-hidden="true"><div>Drop to load</div></div>
    </>
  );
}
