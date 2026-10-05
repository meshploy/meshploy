import { z } from "zod"

// Zod compiles validators with new Function() when it may. The console's CSP
// allows no string evaluation, so it never may; jitless stops it trying, and
// the browser reporting a blocked attempt on every load. Zod reads the setting
// as each schema is built, so main.tsx imports this before any module that
// builds one.
z.config({ jitless: true })
