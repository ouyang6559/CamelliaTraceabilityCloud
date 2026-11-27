// CTC service worker (specs/11.4 PWA): precaches the app shell on install,
// serves static assets cache-first, and passes /api requests straight to the
// network (offline capture is handled by the IndexedDB queue in the app —
// api/offline.ts — so API failures surface to the queue instead of the SW).
const CACHE = 'ctc-shell-v1'

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) => cache.addAll(['/', '/index.html', '/logo.svg', '/manifest.webmanifest'])),
  )
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))),
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== 'GET' || url.pathname.startsWith('/api/')) {
    return // network only
  }
  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached
      return fetch(event.request).then((res) => {
        if (res.ok && url.origin === self.location.origin) {
          const copy = res.clone()
          caches.open(CACHE).then((cache) => cache.put(event.request, copy))
        }
        return res
      }).catch(() => caches.match('/index.html'))
    }),
  )
})
