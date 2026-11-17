import { useState } from "react";
import type { SourceInfo } from "../api";
import { roman, shortPath } from "../lib/files";

const KIND_LABEL = { folder: "Folder", epub: "EPUB", github: "GitHub", path: "On disk" } as const;
const LIMIT = 14;

function partName(info: SourceInfo, idx: number): string {
  if (idx === 0) return "Front matter";
  if (idx >= 999) return "Appendices";
  const p = (info.parts ?? []).find((x) => x.index === idx);
  if (!p || !p.title) return `Part ${roman(idx)}`;
  return /^part\s/i.test(p.title) ? p.title : `Part ${roman(idx)} · ${p.title}`;
}

/** The detected book: title, author and table of contents. */
export function Manifest({ info, onClear }: { info: SourceInfo; onClear: () => void }) {
  const [all, setAll] = useState(false);
  const chapters = info.chapters ?? [];
  const shown = all ? chapters : chapters.slice(0, LIMIT);
  const showParts = new Set(chapters.map((c) => c.part)).size > 1;
  const label = info.kind === "path" ? shortPath(info.label) : info.label;

  const rows: React.ReactNode[] = [];
  let lastPart: number | null = null;
  let n = 0;
  shown.forEach((c, i) => {
    if (showParts && c.part !== lastPart) {
      rows.push(<li key={`p${i}`} className="toc-part">{partName(info, c.part)}</li>);
      lastPart = c.part;
    }
    const numbered = c.part !== 0 && c.part < 999;
    if (numbered) n++;
    rows.push(
      <li key={`c${i}`} className="toc-ch" style={{ animationDelay: `${Math.min(i * 22, 400)}ms` }}>
        <span className="t">{c.title || "Untitled"}</span>
        <span className="lead" aria-hidden="true" />
        <span className="n">{numbered ? n : "·"}</span>
      </li>,
    );
  });

  const rest = chapters.length - LIMIT;
  return (
    <article className="manifest" aria-live="polite">
      <header className="manifest-head">
        <div>
          <p className="eyebrow mf-source">
            <span>{KIND_LABEL[info.kind] ?? info.kind}</span>
            <span className="mf-label" title={info.label}>{label}</span>
          </p>
          <h2 className="mf-title">{info.title || "Untitled manuscript"}</h2>
          {info.author && <p className="mf-author">by {info.author}</p>}
        </div>
        <button className="btn btn-ghost btn-sm" type="button" onClick={onClear}>Change</button>
      </header>
      <p className="mf-count"><span className="num">{chapters.length}</span> chapters found</p>
      <ol className="toc">{rows}</ol>
      {!all && rest > 0 && (
        <button className="link-btn" type="button" onClick={() => setAll(true)}>and {rest} more…</button>
      )}
    </article>
  );
}
