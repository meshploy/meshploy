import { http, HttpResponse, ws } from "msw"

const S = "/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId"
const lines = () => [
  `${new Date().toISOString()} INFO [demo] Application started on port 4000`,
  `${new Date().toISOString()} INFO [demo] Database connection established`,
  `${new Date().toISOString()} INFO [demo] GET /health 200 2ms`,
  `${new Date().toISOString()} INFO [demo] GET /api/projects 200 18ms`,
]
function stream(request: Request, messages: string[]) {
  let timer: ReturnType<typeof setInterval>
  const response = new ReadableStream({
    start(controller) {
      let i = 0
      const stop = () => {
        clearInterval(timer)
        try {
          controller.close()
        } catch {
          /* already closed */
        }
      }
      request.signal.addEventListener("abort", stop, { once: true })
      timer = setInterval(() => {
        if (request.signal.aborted) {
          stop()
          return
        }
        controller.enqueue(
          new TextEncoder().encode(
            i < messages.length
              ? `data: ${messages[i++]}\n\n`
              : "event: done\ndata: complete\n\n"
          )
        )
        if (i >= messages.length) {
          controller.enqueue(
            new TextEncoder().encode("event: done\ndata: complete\n\n")
          )
          request.signal.removeEventListener("abort", stop)
          stop()
        }
      }, 450)
    },
    cancel() {
      clearInterval(timer)
    },
  })
  return new HttpResponse(response, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
    },
  })
}
const terminal = ws.link(/wss?:\/\/.*\/api\/v1\/.*\/terminal(?:\?.*)?$/)
export const streamHandlers = [
  http.get(`${S}/logs/stream`, ({ request }) => stream(request, lines())),
  http.get(
    `${S}/logs`,
    () =>
      new HttpResponse(lines().join("\n"), {
        headers: { "Content-Type": "text/plain" },
      })
  ),
  http.get(`${S}/deployments/:deploymentId/logs/stream`, ({ request }) =>
    stream(request, [
      "[demo] Preparing build context",
      "[demo] Building image…",
      "[demo] Image ready",
      "[demo] Starting application",
      "[demo] Health checks passed. Deployment complete.",
    ])
  ),
  terminal.addEventListener("connection", ({ client }) => {
    let command = ""
    client.send(
      "\x1b[36mMeshploy demo terminal\x1b[0m\r\nSimulated shell. Try help, ls, pwd, whoami, or clear.\r\n\r\ndemo@meshploy:~$ "
    )
    client.addEventListener("message", async (event) => {
      let data = event.data
      if (typeof data === "string" && data.startsWith('{"type":"resize"'))
        return
      if (data instanceof Blob) data = await data.text()
      if (data instanceof ArrayBuffer) data = new TextDecoder().decode(data)
      if (ArrayBuffer.isView(data)) data = new TextDecoder().decode(data)
      for (const char of String(data)) {
        if (char === "\r" || char === "\n") {
          const cmd = command.trim()
          command = ""
          const answers: Record<string, string> = {
            help: "Commands: help, ls, pwd, whoami, clear. This shell never executes commands.",
            ls: "app  config  logs",
            pwd: "/home/demo",
            whoami: "demo",
            "uname -a": "Linux meshploy-demo (simulated)",
          }
          client.send(
            cmd === "clear"
              ? "\x1b[2J\x1b[Hdemo@meshploy:~$ "
              : `\r\n${cmd ? (answers[cmd] || "Command unavailable in this simulated terminal.") + "\r\n" : ""}demo@meshploy:~$ `
          )
        } else if (char === "\x7f") {
          if (command.length) {
            command = command.slice(0, -1)
            client.send("\b \b")
          }
        } else if (char === "\x03") {
          command = ""
          client.send("^C\r\ndemo@meshploy:~$ ")
        } else {
          command += char
          client.send(char)
        }
      }
    })
  }),
]
