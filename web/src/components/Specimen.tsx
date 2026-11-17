/** A miniature page in Folio's default style, shown before anything is built. */
export function Specimen() {
  return (
    <figure className="specimen" aria-label="Sample page in Folio's default style">
      <div className="sp-page">
        <div className="sp-head"><span>Part I · Transport</span><span>7</span></div>
        <p className="sp-chapter">Chapter 1</p>
        <p className="sp-title">Diffusion</p>
        <div className="sp-rule" />
        <p className="sp-text">
          <span className="sp-cap">F</span>ick's second law relates the change in concentration over time to its curvature in
          space, <span className="sp-math">∂ρ/∂t = D ∇²ρ</span>, where <i>D</i> is the diffusion coefficient.
        </p>
        <svg className="sp-diagram" viewBox="0 0 220 54" aria-hidden="true">
          <rect className="sd-a" x="2" y="14" width="56" height="26" rx="4" />
          <rect className="sd-b" x="82" y="14" width="56" height="26" rx="4" />
          <rect className="sd-c" x="162" y="14" width="56" height="26" rx="4" />
          <path className="sd-arrow" d="M60 27h18m-5-4 5 4-5 4M140 27h18m-5-4 5 4-5 4" />
          <text x="30" y="31">Source</text>
          <text x="110" y="31">Medium</text>
          <text x="190" y="31">Sink</text>
        </svg>
        <p className="sp-caption">Figure 1.1. Mass moves from source to sink.</p>
        <p className="sp-text sp-indent">For a point source released at <i>t</i> = 0 on an infinite line, the solution is a Gaussian whose width grows with the square root of time:</p>
        <p className="sp-display">ρ(x, t) = (4πDt)<sup>−½</sup> e<sup>−x²/4Dt</sup></p>
        <p className="sp-text">Doubling the distance therefore takes four times as long, which is why <b>diffusion</b> is fast across a cell and slow across a room.</p>
        <div className="sp-foot">7</div>
      </div>
      <figcaption>Sample page. Diagrams are drawn as vectors, not pasted in as images.</figcaption>
    </figure>
  );
}
