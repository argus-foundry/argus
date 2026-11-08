const paths = {
  action: 'M5 7h14M5 12h14M5 17h14',
  settings: 'M4 7h16M4 17h16M8 4v6m8 4v6',
  pin: 'm8 3 8 0-1 6 4 4v2H5v-2l4-4-1-6zM12 15v6',
  unpin: 'm3 3 18 18M9 3h7l-1 6 4 4v2h-3M8 8l1 1-4 4v2h6m1 1v5',
  copy: 'M9 9h12v12H9zM15 9V3H3v12h6',
  branch: 'M6 3v12a3 3 0 1 0 3 3c0-3 9-3 9-9V6M4 3a2 2 0 1 0 4 0 2 2 0 0 0-4 0M16 4a2 2 0 1 0 4 0 2 2 0 0 0-4 0',
  folder: 'M3 7V5h6l2 2h10v13H3V7z',
  external: 'M14 3h7v7m0-7L10 14M10 3H3v18h18v-7',
  active: 'm9 5 10 7-10 7V5z',
  backlog: 'M4 5h16v14H4zM4 14h5l1 2h4l1-2h5',
  blocked: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18M6 6l12 12',
  done: 'm5 12 4 4L19 6',
  delete: 'M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7',
  notifications: 'M6 16h12l-2-3V9a4 4 0 0 0-8 0v4l-2 3m4 4h4',
  agents: 'M5 10h14v9H5zM12 6v4M9 14h.01M15 14h.01M9 17h6',
  sandbox: 'm12 3 8 4v5c0 4-8 9-8 9s-8-5-8-9V7l8-4z',
  schedules: 'M5 5h14v15H5zM8 3v4m8-4v4M5 10h14m-11 4h3m2 3h3',
  system: 'M4 5h16v12H4zM8 21h8m-4-4v4',
  maintenance: 'm5 19 9-9m0 0a5 5 0 0 0 6-6l-3 3-3-3 3-3a5 5 0 0 0-6 6l-9 9z',
  update: 'M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5',
  logs: 'M6 3h12v18H6zM9 7h6m-6 5h6m-6 5h6',
  add: 'M12 4v16M4 12h16',
};

export function menuIcon(name = 'action') {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24'); svg.setAttribute('aria-hidden', 'true'); svg.setAttribute('focusable', 'false');
  svg.setAttribute('width', '16'); svg.setAttribute('height', '16');
  svg.setAttribute('fill', 'none'); svg.setAttribute('stroke', 'currentColor'); svg.setAttribute('stroke-width', '1.5');
  svg.setAttribute('stroke-linecap', 'round'); svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('class', 'menu-icon');
  const path = document.createElementNS(svg.namespaceURI, 'path'); path.setAttribute('d', Object.hasOwn(paths,name) ? paths[name] : paths.action);
  svg.append(path); return svg;
}
