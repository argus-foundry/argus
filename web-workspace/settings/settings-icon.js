const paths = {
  general: 'M4 7h16M4 17h16M8 4v6m8 4v6',
  notifications: 'M6 16h12l-2-3V9a4 4 0 0 0-8 0v4l-2 3m4 4h4',
  agents: 'M5 10h14v9H5zM12 6v4M9 14h.01M15 14h.01M9 17h6',
  sandbox: 'm12 3 8 4v5c0 4-8 9-8 9s-8-5-8-9V7l8-4z',
  schedules: 'M5 5h14v15H5zM8 3v4m8-4v4M5 10h14m-11 4h3m2 3h3',
  system: 'M4 5h16v12H4zM8 21h8m-4-4v4',
  maintenance: 'm5 19 9-9m0 0a5 5 0 0 0 6-6l-3 3-3-3 3-3a5 5 0 0 0-6 6l-9 9z',
  update: 'M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5',
  logs: 'M6 3h12v18H6zM9 7h6m-6 5h6m-6 5h6',
};
export function settingsIcon(id, label) {
  const icon = document.createElement('span'); icon.className = 'settings-nav-icon'; icon.setAttribute('aria-hidden', 'true');
  if (id.startsWith('repository:')) icon.textContent = label.slice(0, 1).toUpperCase();
  else {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); svg.setAttribute('viewBox', '0 0 24 24');
    const path = document.createElementNS(svg.namespaceURI, 'path'); path.setAttribute('d', paths[id] || 'M12 4v16M4 12h16');
    svg.append(path); icon.append(svg);
  }
  return icon;
}
