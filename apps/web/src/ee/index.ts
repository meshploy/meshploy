// EE slot - intentionally empty in the open-source build.
//
// The EE image build overlays this file with real entries before running Vite,
// so EE routes and nav items appear only in that bundle. Keeping the slot empty
// here means the public repo leaks nothing about EE: no labels, no route names,
// no hints about what the paid tier contains.
//
// This is about licensing more than secrecy. A customer can always read the
// JavaScript they were served; what the empty slot prevents is EE features
// being published under this repository's MIT licence, where anyone - including
// someone who never paid - could legally fork them.
//
// Do not add CE features here.

import type { AnyRoute } from "@tanstack/react-router"

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

/**
 * Routes an edition adds to the console, made in code with `createRoute`:
 * `root` routes hang off the root, with a layout of their own; `console`
 * routes sit inside the console's layout, behind its sign-in. Given the two
 * parents so each route can name its own in `getParentRoute`.
 */
export type EeRouteParents = { root: AnyRoute; console: AnyRoute }
export type EeRouteSet = { root?: AnyRoute[]; console?: AnyRoute[] }
export const eeRoutes = (_parents: EeRouteParents): EeRouteSet => ({})

/**
 * Where the door the console is opened through sends a path, or undefined to
 * serve it as it is. Asked on every navigation, so an edition serving another
 * face on another host can keep that host's visitors on its own pages.
 */
export const eeRedirect = (_pathname: string): string | undefined => undefined

/** Drawn beside a project's name wherever the console lists or heads it. */
export const EeProjectBadge: React.ComponentType<{ projectId: string }> | null = null
