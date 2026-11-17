import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { STAGES, type BuildStream } from "../hooks/useBuildStream";
import { LOWER_ROMAN } from "../lib/files";

interface Props {
  title: string;
  author: string;
  stream: BuildStream;
  startedAt: number;
  onStop: () => void;
  onBack?: () => void; // shown once the build is over, to return to the PDF
}

function lineClass(line: string): string {
  if (/^error|error:|failed|fatal/i.test(line)) return "ln err";
  if (/warning:|overfull|underfull/i.test(line)) return "ln warn";
  if (/^(Generated LaTeX|Verification Gate|Built |Cloning|Extracting)/.test(line)) return "ln step";
  if (/^note:/i.test(line)) return "ln dim";
  return "ln";
}

export function Press({ title, author, stream, startedAt, onStop, onBack }: Props) {
  const running = stream.status === "streaming";
  const [now, setNow] = useState(() => performance.now());
  const [stopping, setStopping] = useState(false);
  const logRef = useRef<HTMLPreElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setNow(performance.now()), 100);
    return () => clearInterval(t);
  }, [running]);

  // Follow the log unless the reader has scrolled up.
  useLayoutEffect(() => {
    const el = logRef.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [stream.lines.length]);

  const currentIdx = STAGES.findIndex((s) => s.id === stream.stage);
  const stageClass = (id: string, i: number) => {
    const seen = stream.seen.includes(id);
    if (stream.status === "done") return seen ? "done" : "skipped";
    if (i < currentIdx) return seen ? "done" : "skipped";
    if (i === currentIdx) return running ? "active" : "";
    return "";
  };

  return (
    <div className="state">
      <p className="section-label">{running ? "Building" : "Build log"}</p>
      <h2 className="h-serif">{title || "Untitled"}</h2>
      <p className="muted" id="press-author">{author ? `by ${author}` : ""}</p>

      <ol className="stages">
        {STAGES.map((s, i) => (
          <li key={s.id} className={stageClass(s.id, i)}>
            <span className="st-num">{LOWER_ROMAN[i]}</span>
            <span className="st-name">{s.name}</span>
          </li>
        ))}
      </ol>

      <div className="press-meta">
        <span className="num" id="press-clock">
          {(stream.result?.result && !running ? stream.result.result.seconds : Math.max(0, now - startedAt) / 1000).toFixed(1)}s
        </span>
        {running && (
          <button
            className="btn btn-ghost btn-sm"
            type="button"
            disabled={stopping}
            onClick={async () => {
              setStopping(true);
              try { onStop(); } finally { setStopping(false); }
            }}
          >
            Cancel
          </button>
        )}
        {!running && onBack && (
          <button className="btn btn-ghost btn-sm" type="button" onClick={onBack}>Back to PDF</button>
        )}
      </div>

      <div className="galley">
        <div className="galley-head">
          <span>Log</span>
          <span className="num">{stream.lines.length} {stream.lines.length === 1 ? "line" : "lines"}</span>
        </div>
        <pre
          className="galley-log"
          ref={logRef}
          tabIndex={0}
          aria-label="Build log"
          onScroll={(e) => {
            const el = e.currentTarget;
            stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
          }}
        >
          {stream.lines.map((line, i) => (
            <span key={i} className={lineClass(line)}>{line + "\n"}</span>
          ))}
        </pre>
      </div>
    </div>
  );
}
