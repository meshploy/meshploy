// EE slot for demo mode - intentionally empty in the open-source build.
//
// The request handlers an edition adds to the demo console's mock API, for the
// routes only its own API serves. Loaded only in demo mode.

import type { RequestHandler } from "msw"

export const eeMockHandlers: RequestHandler[] = []
