/** Meshploy's mark: the nodes of the M, joined through the one in the middle.
 *  Drawn at the weights of meshploy.com's mark, so it reads at 20px without a
 *  tile behind it. */
export function MeshMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 100 100" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden>
      <polyline points="18,78 18,22 50,58 82,22 82,78" strokeWidth="7" />
      <line x1="18" y1="78" x2="50" y2="58" strokeWidth="4" opacity="0.45" />
      <line x1="82" y1="78" x2="50" y2="58" strokeWidth="4" opacity="0.45" />
      <g fill="currentColor" stroke="none">
        <circle cx="18" cy="78" r="7" />
        <circle cx="18" cy="22" r="7" />
        <circle cx="50" cy="58" r="7" />
        <circle cx="82" cy="22" r="7" />
        <circle cx="82" cy="78" r="7" />
      </g>
    </svg>
  )
}
