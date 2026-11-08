export function railChevron(className) {
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  icon.setAttribute('viewBox', '0 0 12 12');
  icon.setAttribute('class', `rail-chevron ${className}`);
  icon.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(icon.namespaceURI, 'path');
  path.setAttribute('d', 'm4 2 4 4-4 4');
  icon.append(path);
  return icon;
}
