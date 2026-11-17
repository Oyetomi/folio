import type { FormEvent } from "react";
import type { FontMode } from "../api";

export interface TitlePageValues {
  title: string;
  subtitle: string;
  author: string;
  index: boolean;
  verify: boolean;
  fonts: FontMode;
}

const TYPEFACES: { mode: FontMode; name: string; note: string }[] = [
  { mode: "open", name: "Open", note: "Crimson Pro and Source Sans. Free; builds the same anywhere." },
  { mode: "pro", name: "Pro", note: "Minion and Myriad from ~/.folio/fonts. This computer only." },
  { mode: "mono", name: "Mono", note: "Ubuntu Mono throughout." },
];

interface Props {
  values: TitlePageValues;
  onChange: (v: TitlePageValues) => void;
  onSubmit: () => void;
  canBuild: boolean;
  auditAvailable: boolean;
  proAvailable: boolean;
  busy: boolean;          // a build is running
  disabledReason: string; // shown next to a disabled button
  rebuild: boolean;       // this source was built before
}

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export function TitlePage({ values, onChange, onSubmit, canBuild, auditAvailable, proAvailable, busy, disabledReason, rebuild }: Props) {
  const set = <K extends keyof TitlePageValues>(k: K, v: TitlePageValues[K]) => onChange({ ...values, [k]: v });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit();
  };

  return (
    <form className="titlepage" autoComplete="off" onSubmit={submit}>
      <div className="step">
        <span className="step-num">2</span>
        <span className="step-title">Title page and options</span>
      </div>
      <div className="fields">
        <label className="field">
          <span className="field-label">Title</span>
          <input type="text" placeholder="Detected from the source" value={values.title} onChange={(e) => set("title", e.target.value)} />
        </label>
        <label className="field">
          <span className="field-label">Subtitle</span>
          <input type="text" placeholder="Optional" value={values.subtitle} onChange={(e) => set("subtitle", e.target.value)} />
        </label>
        <label className="field field-wide">
          <span className="field-label">Author</span>
          <input type="text" placeholder="Detected from the source" value={values.author} onChange={(e) => set("author", e.target.value)} />
        </label>
      </div>

      <fieldset className="typefaces">
        <legend className="field-label">Typeface</legend>
        {TYPEFACES.map((t) => {
          const disabled = t.mode === "pro" && !proAvailable;
          return (
            <label key={t.mode} className={"typeface" + (values.fonts === t.mode ? " on" : "")} aria-disabled={disabled || undefined}>
              <input
                type="radio"
                name="fonts"
                value={t.mode}
                checked={values.fonts === t.mode}
                disabled={disabled}
                onChange={() => set("fonts", t.mode)}
              />
              <span className="tf-check" aria-hidden="true">
                <svg viewBox="0 0 16 16"><path d="M4 8.4l2.6 2.5L12 5.4" /></svg>
              </span>
              <strong>{t.name}</strong>
              <small>{disabled ? "Not installed in ~/.folio/fonts." : t.note}</small>
            </label>
          );
        })}
      </fieldset>

      <div className="switches">
        <Switch
          checked={values.index}
          onChange={(v) => set("index", v)}
          title="Back-of-book index"
          note="Collects bold term definitions. Turn off for fiction."
        />
        <Switch
          checked={auditAvailable && values.verify}
          disabled={!auditAvailable}
          onChange={(v) => set("verify", v)}
          title="Check figures"
          note={auditAvailable ? "Flags overlapping labels in diagrams" : "Requires Poppler (brew install poppler)"}
        />
      </div>

      <div className="press-row">
        <button className={"btn btn-press" + (busy ? " busy" : "")} type="submit" disabled={!canBuild} aria-busy={busy}>
          {busy && <span className="spinner" aria-hidden="true" />}
          {busy ? "Building…" : rebuild ? "Rebuild PDF" : "Build PDF"}
        </button>
        {!canBuild && !busy && disabledReason ? (
          <span className="press-hint">{disabledReason}</span>
        ) : (
          <span className="kbd-hint" aria-hidden="true"><kbd>{isMac ? "⌘" : "Ctrl"}</kbd><kbd>↵</kbd></span>
        )}
      </div>
    </form>
  );
}

function Switch(props: { checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; title: string; note: string }) {
  return (
    <label className="switch">
      <input type="checkbox" checked={props.checked} disabled={props.disabled} onChange={(e) => props.onChange(e.target.checked)} />
      <span className="switch-track" aria-hidden="true"><span className="switch-thumb" /></span>
      <span><strong>{props.title}</strong><small>{props.note}</small></span>
    </label>
  );
}
