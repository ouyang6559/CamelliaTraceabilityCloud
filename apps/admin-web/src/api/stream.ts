// SSE realtime client (specs/09.10 Realtime): subscribes /api/v1/stream
// (tenant-scoped NATS bridge) and dispatches domain events to listeners.
// Reconnects automatically with backoff; page visibility pauses consumption
// (the browser buffers server-side anyway).
export type StreamHandler = (event: string, data: Record<string, unknown>) => void

let source: EventSource | null = null
let backoff = 1000
const handlers = new Set<StreamHandler>()

export function subscribeStream(handler: StreamHandler): () => void {
  handlers.add(handler)
  ensureConnection()
  return () => {
    handlers.delete(handler)
    if (handlers.size === 0) closeStream()
  }
}

function ensureConnection() {
  if (source) return
  source = new EventSource('/api/v1/stream')
  source.onopen = () => {
    backoff = 1000
  }
  // Domain events are emitted as named events: mission.updated, alert.created,
  // device.telemetry, device.status, remote-sensing.updated, sync.updated...
  source.onmessage = (ev) => dispatch('message', ev.data)
  const domains = [
    'mission.updated', 'mission.started', 'alert.created', 'device.telemetry',
    'device.status', 'remote-sensing.updated', 'remote-sensing.stac',
    'sync.updated', 'proof.created', 'agri.task.updated', 'trace.created',
    'proof.created',
  ]
  for (const name of domains) {
    source.addEventListener(name, (ev) => {
      dispatch(name, (ev as MessageEvent).data)
    })
  }
  source.onerror = () => {
    closeStream()
    // Exponential backoff reconnect while handlers remain.
    if (handlers.size > 0) {
      setTimeout(ensureConnection, backoff)
      backoff = Math.min(backoff * 2, 30_000)
    }
  }
}

function dispatch(event: string, rawData: string) {
  let data: Record<string, unknown> = {}
  try {
    data = JSON.parse(rawData)
  } catch {
    return
  }
  handlers.forEach((h) => h(event, data))
}

function closeStream() {
  if (source) {
    source.close()
    source = null
  }
}
