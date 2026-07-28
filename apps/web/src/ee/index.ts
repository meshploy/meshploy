// EE slot — intentionally empty in the open-source build.
//
// The EE image build overlays this file with real entries before running Vite,
// so EE routes and nav items appear only in that bundle. Keeping the slot empty
// here means the public repo leaks nothing about EE: no labels, no route names,
// no hints about what the paid tier contains.
//
// Do not add CE features here.

import type { ReactNode } from "react"

export type EeNavItem = {
  href: string
  label: string
  icon?: ReactNode
  /** Feature flag from GET /api/v1/entitlements that must be present to show this. */
  feature?: string
}

export const eeNavItems: EeNavItem[] = []
