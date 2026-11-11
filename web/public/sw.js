/* Puck task notifications.
   This worker only displays notifications. It does not cache or intercept fetches.
   Closed-tab delivery still needs VAPID Web Push; see docs/specs/0130 §10.11. */
self.addEventListener("push", (event) => {
  let payload = {};
  try { payload = event.data ? event.data.json() : {}; } catch { payload = {}; }
  const title = String(payload.title || "Puck").slice(0, 40);
  const body = String(payload.body || "").slice(0, 180);
  const tag = String(payload.tag || "puck-task");
  const href = typeof payload.href === "string" && payload.href.startsWith("/") ? payload.href : "/";
  event.waitUntil(self.registration.showNotification(title, { body, tag, data: { href } }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const href = event.notification.data && typeof event.notification.data.href === "string" ? event.notification.data.href : "/";
  event.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
    for (const client of clients) {
      if ("focus" in client) {
        if ("navigate" in client && href) client.navigate(href);
        return client.focus();
      }
    }
    if (self.clients.openWindow) return self.clients.openWindow(href);
    return undefined;
  }));
});

self.addEventListener("message", (event) => {
  const data = event.data || {};
  if (data.type !== "puck-notify") return;
  const title = String(data.title || "Puck").slice(0, 40);
  const body = String(data.body || "").slice(0, 180);
  const tag = String(data.tag || "puck-task");
  event.waitUntil(self.registration.showNotification(title, { body, tag, data: { href: "/" } }));
});
