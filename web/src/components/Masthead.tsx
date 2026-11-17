interface Props {
  onToggleTheme: () => void;
}

export function Masthead({ onToggleTheme }: Props) {
  return (
    <header className="masthead rise d1">
      <a className="brand" href="/" aria-label="Folio home">
        <svg className="brand-mark" viewBox="0 0 40 40" aria-hidden="true">
          <rect x="3" y="3" width="34" height="34" rx="5" className="bm-plate" />
          <path className="bm-leaf" d="M13 11h15v3.4H17v4.4h9.2v3.3H17V29h-4z" />
          <circle className="bm-dot" cx="29" cy="28" r="2.2" />
        </svg>
        <span className="brand-word">Folio</span>
      </a>
      <button className="lamp" type="button" aria-label="Toggle dark mode" title="Dark mode" onClick={onToggleTheme}>
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path className="lamp-shade" d="M7 4h10l3 8H4z" />
          <path className="lamp-stem" d="M12 12v6M8 20h8" />
        </svg>
      </button>
    </header>
  );
}
