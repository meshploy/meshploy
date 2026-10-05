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
 * Where the door the console is opened through sends a location (its path
 * and query), or undefined to serve it as it is. Asked on every navigation, so
 * an edition serving another face on another host can keep that host's
 * visitors on its own pages.
 */
export const eeRedirect = (_href: string): string | undefined => undefined

/** Drawn beside a project's name wherever the console lists or heads it. */
export const EeProjectBadge: React.ComponentType<{ projectId: string }> | null = null

/**
 * Drawn at the top of a service's Access page: who outside the team may reach
 * the service. With it, the team's permissions sit below under their own
 * heading.
 */
export const EeServiceAccess: React.ComponentType<{ orgId: string; projectId: string; serviceId: string }> | null = null

/** Drawn in the side column of a member's page, for what an edition knows about them. */
export const EeMemberPanel: React.ComponentType<{ orgId: string; userId: string }> | null = null

/**
 * Drawn on the Access page in place of what a grant opens on the mesh, for a
 * grant held by someone outside the organisation: they own no machine on the
 * mesh, so an edition that grants outsiders says what their grant is for.
 */
export const EeOutsiderGrant: React.ComponentType<{ orgId: string; email?: string; resourceKind: string; resourceId?: string; projectId?: string }> | null = null
