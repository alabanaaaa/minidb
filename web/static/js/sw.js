const CACHE_NAME = 'minidb-v1';
const APP_SHELL = [
  '/',
  '/login',
  '/dashboard',
  '/pos',
  '/inventory',
  '/workers',
  '/sessions',
  '/reports',
  '/settings',
  '/admin/users',
  'https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap',
  'https://cdn.jsdelivr.net/npm/chart.js@4.4.7/dist/chart.umd.min.js',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(APP_SHELL).catch((err) => {
        console.warn('Cache install failed for some assets:', err);
      });
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k))
      );
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // API calls — network first, fallback to IndexedDB
  if (url.pathname.startsWith('/api/') || url.pathname === '/pos/checkout') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (response.ok) {
            const clone = response.clone();
            // Store successful API responses
            if (url.pathname.startsWith('/api/charts/')) {
              caches.open(CACHE_NAME).then((cache) => {
                cache.put(request, clone);
              });
            }
          }
          return response;
        })
        .catch(() => {
          // Offline: return cached response or empty data
          return caches.match(request).then((cached) => {
            if (cached) return cached;
            // Return empty JSON for chart APIs
            if (url.pathname.startsWith('/api/charts/')) {
              return new Response(JSON.stringify({ data: [] }), {
                headers: { 'Content-Type': 'application/json' },
              });
            }
            return new Response(JSON.stringify({ offline: true }), {
              headers: { 'Content-Type': 'application/json' },
            });
          });
        })
    );
    return;
  }

  // Static assets — cache first
  if (
    request.destination === 'style' ||
    request.destination === 'script' ||
    request.destination === 'image' ||
    request.destination === 'font'
  ) {
    event.respondWith(
      caches.match(request).then((cached) => {
        return cached || fetch(request).then((response) => {
          if (response.ok) {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
          }
          return response;
        });
      })
    );
    return;
  }

  // HTML pages — network first, fallback to cache
  event.respondWith(
    fetch(request).then((response) => {
      if (response.ok) {
        const clone = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
      }
      return response;
    }).catch(() => {
      return caches.match(request).then((cached) => {
        return cached || caches.match('/dashboard');
      });
    })
  );
});

// Sync when back online
self.addEventListener('sync', (event) => {
  if (event.tag === 'sync-sales') {
    event.waitUntil(syncPendingSales());
  }
});

async function syncPendingSales() {
  const db = await openDB();
  const tx = db.transaction('pending_sales', 'readonly');
  const store = tx.objectStore('pending_sales');
  const pending = await store.getAll();

  for (const sale of pending) {
    try {
      await fetch('/pos/checkout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams(sale.data),
      });
      // Remove from pending
      const deleteTx = db.transaction('pending_sales', 'readwrite');
      await deleteTx.objectStore('pending_sales').delete(sale.id);
    } catch (err) {
      console.warn('Failed to sync sale:', sale.id, err);
    }
  }
}

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('minidb-offline', 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains('pending_sales')) {
        db.createObjectStore('pending_sales', { keyPath: 'id' });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}
