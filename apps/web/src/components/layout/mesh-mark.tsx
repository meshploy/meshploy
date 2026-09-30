/** Meshploy's mark: the nodes of the M, joined through the one in the middle. */
export function MeshMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 100 100" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <polyline points="18,78 18,22 50,58 82,22 82,78" />
      <line x1="18" y1="78" x2="50" y2="58" opacity="0.45" />
      <line x1="82" y1="78" x2="50" y2="58" opacity="0.45" />
      <circle cx="18" cy="78" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="18" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="50" cy="58" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="78" r="5.5" fill="currentColor" stroke="none" />
    </svg>
  )
}
