self.addEventListener('push', event => {
  let data = {};
  try { data = event.data?.json() || {}; } catch { /* Invalid payloads still produce a visible notification. */ }
  const task = typeof data.taskId === 'string' ? data.taskId : '';
  event.waitUntil(self.registration.showNotification(typeof data.title === 'string' ? data.title : 'Argus', {
    body: typeof data.body === 'string' ? data.body : 'Agent needs attention', icon: '/icon-192.png', badge: '/icon-192.png', data: { taskId: task }, tag: task || 'argus',
  }));
});
self.addEventListener('notificationclick', event => {
  event.notification.close();
  const url = new URL('/', self.location.origin), task = event.notification.data?.taskId;
  if (typeof task === 'string' && task) url.searchParams.set('task', task);
  event.waitUntil(self.clients.openWindow(url.href));
});
