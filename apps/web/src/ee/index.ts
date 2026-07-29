// EE slot — intentionally empty in the open-source build.
//
// The EE image build overlays this file with real entries before running Vite,
// so EE routes and nav items appear only in that bundle. Keeping the slot empty
// here means the public repo leaks nothing about EE: no labels, no route names,
// no hints about what the paid tier contains.
//
// This is about licensing more than secrecy. A customer can always read the
// JavaScript they were served; what the empty slot prevents is EE features
// being published under this repository's MIT licence, where anyone — including
// someone who never paid — could legally fork them.
//
// Do not add CE features here.

export type EeNavItem = {
  href: string
  /** Same shape as the sidebar's built-in items: a component, not an element. */
  icon: React.ElementType
  label: string
  exact?: boolean
  /**
   * Entitlement flag from GET /api/v1/entitlements that must be present for
   * this item to render. Omit for items any licensed install may see.
   *
   * A licence covering some features but not others therefore shows only what
   * it paid for, without the overlay carrying its own gating logic.
   */
  feature?: string
}

export const eeNavItems: EeNavItem[] = []
