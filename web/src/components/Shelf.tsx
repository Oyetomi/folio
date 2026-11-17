import { useEffect, useRef, type CSSProperties } from "react";
import { isActive, type Job } from "../api";
import { hash, initials } from "../lib/files";

const SPINE_COLORS = ["#8e0012", "#2e5b88", "#3e7048", "#9e5810", "#4a3870", "#2c3e50", "#6d2b3d", "#355e5b"];

interface Props {
  jobs: Job[];
  activeId: string | null;
  onOpen: (id: string) => void;
}

/** Finished (and in-progress) books standing as spines on a plank. */
export function Shelf({ jobs, activeId, onOpen }: Props) {
  const ref = useRef<HTMLElement>(null);

  // Scroll-reveal the shelf the first time it enters the viewport.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            e.target.classList.add("in");
            io.unobserve(e.target);
          }
        }
      },
      { threshold: 0.15 },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);

  const books = jobs.filter((j) => j.state === "done" || isActive(j)).slice().reverse();

  return (
    <section className="shelf-wrap reveal" aria-labelledby="shelf-title" ref={ref}>
      <div className="shelf-head">
        <h2 className="h-serif" id="shelf-title">Built this session</h2>
        <p className="muted">Click a spine to reopen its PDF. Books are kept until you stop the server.</p>
      </div>
      <div className="shelf" role="list">
        {books.length === 0 && (
          <p className="shelf-empty">Books you build will appear here.</p>
        )}
        {books.map((j, i) => {
          const binding = isActive(j);
          const pages = j.result?.pages ?? 0;
          const h = hash(j.id + (j.title || ""));
          const title = j.title || "Untitled";
          const style = {
            "--c": SPINE_COLORS[h % SPINE_COLORS.length],
            "--w": `${Math.round(Math.min(64, Math.max(30, 28 + Math.sqrt(pages || 16) * 3)))}px`,
            "--h": `${212 + (h % 56)}px`,
            animationDelay: `${Math.min(i, 12) * 40}ms`, // only plays on mount
          } as CSSProperties;
          return (
            <button
              key={j.id + (binding ? ":b" : "")}
              type="button"
              role="listitem"
              className={"spine" + (binding ? " binding" : "")}
              style={style}
              aria-current={j.id === activeId}
              title={binding ? `${title} (building)` : `${title}${pages ? ` · ${pages} pages` : ""}`}
              aria-label={binding ? `${title}, building` : `Open ${title}`}
              onClick={() => onOpen(j.id)}
            >
              <span className="sp-t">{title}</span>
              <span className="sp-a">{initials(j.author || "")}</span>
            </button>
          );
        })}
      </div>
      <div className="plank" aria-hidden="true" />
    </section>
  );
}
