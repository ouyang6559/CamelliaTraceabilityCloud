// PWA offline capture queue (specs/11.4): task completions and pest reports
// captured offline land in IndexedDB and flush automatically when connectivity
// returns (online event + app start). Minimal dependency-free IndexedDB shim.
export interface OfflineItem {
  id: string
  kind: 'task-complete' | 'pest-report'
  endpoint: string
  body: unknown
  queuedAt: string
}

const DB_NAME = 'ctc-offline'
const STORE = 'queue'

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'id' })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

export async function queueLength(): Promise<number> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).count()
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

export async function enqueue(item: Omit<OfflineItem, 'id' | 'queuedAt'>): Promise<void> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    const full: OfflineItem = {
      ...item,
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      queuedAt: new Date().toISOString(),
    }
    tx.objectStore(STORE).add(full)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

export async function listQueue(): Promise<OfflineItem[]> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readonly')
    const req = tx.objectStore(STORE).getAll()
    req.onsuccess = () => resolve((req.result as OfflineItem[]).sort((a, b) => a.queuedAt.localeCompare(b.queuedAt)))
    req.onerror = () => reject(req.error)
  })
}

async function remove(id: string): Promise<void> {
  const db = await openDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).delete(id)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

// flushQueue replays queued items in order; each success removes the item, a
// 401 pauses the flush (token refresh happens in the api client), other
// failures keep the item for retry. Returns the number of flushed items.
export async function flushQueue(): Promise<number> {
  const items = await listQueue()
  let flushed = 0
  for (const item of items) {
    try {
      const res = await fetch(item.endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(item.body),
      })
      if (res.ok) {
        await remove(item.id)
        flushed++
      } else if (res.status === 401) {
        break // auth expired: let the api client refresh, retry next flush
      }
      // 4xx validation errors keep the item (user can inspect via listQueue);
      // wholesale deletion of invalid payloads would silently drop field data.
    } catch {
      break // network still down: retry on the next flush
    }
  }
  return flushed
}

// Auto-flush on connectivity regain and periodically while online.
let flushing = false
export function startAutoFlush(onFlushed?: (n: number) => void): () => void {
  const run = async () => {
    if (flushing || !navigator.onLine) return
    flushing = true
    try {
      const n = await flushQueue()
      if (n > 0) onFlushed?.(n)
    } finally {
      flushing = false
    }
  }
  window.addEventListener('online', run)
  const timer = setInterval(run, 60_000)
  void run()
  return () => {
    window.removeEventListener('online', run)
    clearInterval(timer)
  }
}

// Aliases consumed by pages (keeps import names stable).
export const offlineEnqueue = enqueue
export const offlineLen = queueLength
