// Minimal pass-through Service Worker for PWA installability without caching sensitive LAN endpoints.
self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('fetch', (event) => {
  // Always hit network directly for LAN API, WebSockets, and reverse proxies.
  event.respondWith(fetch(event.request));
});
