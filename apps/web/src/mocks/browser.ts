import { setupWorker } from "msw/browser"
import { handlers } from "./handlers"
import { eeMockHandlers } from "@/ee/mocks"

// An edition's own routes first, so they answer before any catch-all.
export const worker = setupWorker(...eeMockHandlers, ...handlers)
