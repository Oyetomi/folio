import { useEffect, useState } from "react";
import { artifactUrl, type Job } from "../api";

/** Counts from 0 to target once, for the stats row. Respects reduced motion. */
function useCountUp(target: number, decimals = 0, ms = 700): string {
  const [v, setV] = useState(0);
  useEffect(() => {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setV(target);
      return;
    }
    let raf = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / ms);
      setV(target * (1 - Math.pow(1 - t, 3)));
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [target, ms]);
  return v.toFixed(decimals);
}

function Count({ value, decimals = 0 }: { value: number; decimals?: number }) {
  return <>{useCountUp(value, decimals)}</>;
}

interface StatProps {
  label: string;
  value: React.ReactNode;
  unit?: string;
  tag?: { cls: string; text: string };
}

function Stat({ label, value, unit, tag }: StatProps) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}{unit && <small>{unit}</small>}</dd>
      {tag && <span className={`tag ${tag.cls}`}>{tag.text}</span>}
    </div>
  );
}

/** The finished book: stats, inline PDF and downloads. */
export function ProofView({ job, onShowLog }: { job: Job; onShowLog?: () => void }) {
  const r = job.result;
  const size = r?.sizeBytes ?? 0;
  const [sizeVal, sizeUnit] = size >= 1024 * 1024 ? [(size / 1048576).toFixed(2), "MB"] : [(size / 1024).toFixed(0), "KB"];
  const audit = !r?.verified
    ? { cls: "tag-off", text: "not checked" }
    : r.overlaps === 0
      ? undefined
      : { cls: "tag-warn", text: "review" };

  return (
    <div className="state">
      <header className="proof-head">
        <h2 className="h-serif">{job.title || "Untitled"}</h2>
        {r?.subtitle && <p className="pf-sub">{r.subtitle}</p>}
        {job.author && <p className="pf-author">{job.author}</p>}
      </header>

      <dl className="stats">
        <Stat label="Pages" value={r?.verified ? <Count value={r.pages} /> : "—"} />
        <Stat label="Chapters" value={<Count value={r?.chapters ?? 0} />} />
        <Stat label="Figures" value={<Count value={r?.figures ?? 0} />} />
        <Stat label="Overlaps" value={r?.verified ? <Count value={r.overlaps} /> : "—"} tag={audit} />
        <Stat label="Size" value={<Count value={Number(sizeVal)} decimals={sizeUnit === "MB" ? 2 : 0} />} unit={sizeUnit} />
        <Stat label="Time" value={<Count value={r?.seconds ?? 0} decimals={1} />} unit="s" />
      </dl>

      {!!r?.missingGlyphs && (
        <div className="notice notice-warn glyph-notice" role="status">
          <strong>{r.missingGlyphs} {r.missingGlyphs === 1 ? "character is" : "characters are"} missing from the PDF.</strong>{" "}
          No installed font has {r.missingGlyphs === 1 ? "it" : "them"}:
          <span className="glyph-sample" lang="und">{r.missingSample}</span>
          Emoji and right-to-left scripts (Arabic, Hebrew) are not supported yet. For other scripts, install a font that covers them
          (for example Noto Serif CJK) or put one in <code>~/.folio/fonts</code>, then rebuild.
        </div>
      )}

      <div className="proof-frame">
        {/* key forces a fresh viewer when switching books from the shelf */}
        <iframe key={job.id} src={`${artifactUrl(job.id, "pdf")}#navpanes=0&view=FitH`} title={`PDF proof of ${job.title || "the book"}`} />
      </div>

      <div className="actions">
        <a className="btn btn-press btn-md" href={artifactUrl(job.id, "pdf", true)} download>Download PDF</a>
        <a className="btn btn-ghost btn-md" href={artifactUrl(job.id, "tex", true)} download>Download .tex</a>
        <a className="btn btn-ghost btn-md" href={artifactUrl(job.id, "pdf")} target="_blank" rel="noopener">Open in new tab</a>
        {onShowLog && <button className="btn btn-ghost btn-md" type="button" onClick={onShowLog}>View log</button>}
      </div>
    </div>
  );
}
