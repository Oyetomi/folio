import { useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import type { SourceKind } from "../api";
import type { PathedFile } from "../lib/files";

const TABS: { kind: SourceKind; label: string }[] = [
  { kind: "folder", label: "Folder" },
  { kind: "epub", label: "EPUB" },
  { kind: "github", label: "GitHub" },
  { kind: "path", label: "Local path" },
];

interface Props {
  kind: SourceKind;
  onKind: (k: SourceKind) => void;
  onFolder: (files: PathedFile[]) => void;
  onEpub: (file: File) => void;
  onGitHub: (url: string) => void;
  onPath: (path: string) => void;
  gitAvailable: boolean;
}

export function SourcePicker({ kind, onKind, onFolder, onEpub, onGitHub, onPath, gitAvailable }: Props) {
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [indicator, setIndicator] = useState({ x: 0, w: 0 });

  // Slide one underline to the selected tab; re-measure on resize.
  useLayoutEffect(() => {
    const measure = () => {
      const el = tabRefs.current[TABS.findIndex((t) => t.kind === kind)];
      if (el) setIndicator({ x: el.offsetLeft + 12, w: el.offsetWidth - 24 });
    };
    measure();
    addEventListener("resize", measure);
    return () => removeEventListener("resize", measure);
  }, [kind]);
  const [gh, setGh] = useState("");
  const [localPath, setLocalPath] = useState("");
  // Hide the "github.com/" prefix once the user pastes a full or GitBook URL.
  const fullUrl = /^(https?:\/\/|github\.com\/)|\.gitbook\.io/i.test(gh.trim());

  const onTabKey = (e: KeyboardEvent, i: number) => {
    const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!d) return;
    e.preventDefault();
    const next = (i + d + TABS.length) % TABS.length;
    onKind(TABS[next].kind);
    tabRefs.current[next]?.focus();
  };

  const submitGh = (e: FormEvent) => {
    e.preventDefault();
    let v = gh.trim();
    if (!v) return;
    if (!/^(https?:\/\/|github\.com\/|[^/]+\.gitbook\.io)/i.test(v)) v = "github.com/" + v.replace(/^\/+/, "");
    onGitHub(v);
  };

  const submitPath = (e: FormEvent) => {
    e.preventDefault();
    if (localPath.trim()) onPath(localPath.trim());
  };

  return (
    <>
      <div className="tabs" role="tablist" aria-label="Manuscript source">
        {TABS.map((t, i) => (
          <button
            key={t.kind}
            ref={(el) => { tabRefs.current[i] = el; }}
            role="tab"
            id={`tab-${t.kind}`}
            aria-controls={`panel-${t.kind}`}
            aria-selected={kind === t.kind}
            tabIndex={kind === t.kind ? 0 : -1}
            onClick={() => onKind(t.kind)}
            onKeyDown={(e) => onTabKey(e, i)}
          >
            {t.label}
          </button>
        ))}
        <span className="tab-indicator" aria-hidden="true" style={{ width: indicator.w, transform: `translateX(${indicator.x}px)` }} />
      </div>

      <div className="panels">
        {kind === "folder" && (
          <div role="tabpanel" id="panel-folder" aria-labelledby="tab-folder" className="panel">
            <Dropzone icon="folder" title="Drop a book folder">
              <input
                type="file"
                className="sr-only"
                multiple
                // webkitdirectory isn't in React's typings; spread keeps TS happy.
                {...{ webkitdirectory: "" }}
                onChange={(e) => {
                  const files = [...(e.target.files || [])];
                  e.target.value = "";
                  onFolder(files.map((file) => ({ file, path: file.webkitRelativePath || file.name })));
                }}
              />
              or <u>choose one</u>. Only Markdown, notebooks, config files and images are uploaded.
            </Dropzone>
          </div>
        )}

        {kind === "epub" && (
          <div role="tabpanel" id="panel-epub" aria-labelledby="tab-epub" className="panel">
            <Dropzone icon="book" title="Drop an .epub file">
              <input
                type="file"
                className="sr-only"
                accept=".epub,application/epub+zip"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  e.target.value = "";
                  if (f) onEpub(f);
                }}
              />
              or <u>choose one</u>. The EPUB's cover becomes the PDF cover.
            </Dropzone>
          </div>
        )}

        {kind === "github" && (
          <div role="tabpanel" id="panel-github" aria-labelledby="tab-github" className="panel">
            <form className="inline-form" onSubmit={submitGh}>
              <label className="field-label" htmlFor="gh-url">Public repository</label>
              <div className="affix">
                {!fullUrl && <span className="affix-pre" aria-hidden="true">github.com/</span>}
                <input
                  id="gh-url" type="text" inputMode="url" autoComplete="off" spellCheck={false}
                  placeholder="owner/repository" required value={gh} onChange={(e) => setGh(e.target.value)}
                />
                <button className="btn btn-ink" type="submit" disabled={!gitAvailable}>Fetch</button>
              </div>
              {!gitAvailable && <p className="notice notice-warn">git is not installed, so repositories can't be fetched. Install git, then reload this page.</p>}
              <p className="hint">
                Public repositories only. GitBook links like <code>owner.gitbook.io/book</code> also work.
              </p>
            </form>
          </div>
        )}

        {kind === "path" && (
          <div role="tabpanel" id="panel-path" aria-labelledby="tab-path" className="panel">
            <form className="inline-form" onSubmit={submitPath}>
              <label className="field-label" htmlFor="local-path">Folder or .epub on this computer</label>
              <div className="affix">
                <input
                  id="local-path" type="text" autoComplete="off" spellCheck={false}
                  placeholder="~/books/my-novel" required value={localPath} onChange={(e) => setLocalPath(e.target.value)}
                />
                <button className="btn btn-ink" type="submit">Open</button>
              </div>
              <p className="hint">Files are read in place, so edits show up on the next build.</p>
            </form>
          </div>
        )}
      </div>
    </>
  );
}

const ICONS = {
  folder: "M3 7.5A1.5 1.5 0 0 1 4.5 6h4.4l1.8 2h8.8A1.5 1.5 0 0 1 21 9.5v8A1.5 1.5 0 0 1 19.5 19h-15A1.5 1.5 0 0 1 3 17.5z",
  book: "M5 4.5h9a3 3 0 0 1 3 3V20H8a3 3 0 0 1-3-3zM17 7.5h2V20h-2M5 17a3 3 0 0 1 3-3h9",
};

function Dropzone({ icon, title, children }: { icon: keyof typeof ICONS; title: string; children: React.ReactNode }) {
  const [over, setOver] = useState(false);
  return (
    <label
      className={"dropzone" + (over ? " over" : "")}
      onDragEnter={() => setOver(true)}
      onDragLeave={() => setOver(false)}
      onDrop={() => setOver(false)}
    >
      <svg className="dz-icon" viewBox="0 0 24 24" aria-hidden="true"><path d={ICONS[icon]} /></svg>
      <span className="dz-title">{title}</span>
      <span className="dz-hint">{children}</span>
    </label>
  );
}
