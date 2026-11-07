const installed = new WeakMap();

export function tooltipPosition(target, size, viewport) {
  const gap = 8;
  const clamp = (value, maximum) => Math.max(gap, Math.min(value, maximum - gap));
  const above = target.top - size.height - gap;
  return {
    left: clamp(target.left + target.width / 2 - size.width / 2, viewport.width - size.width),
    top: clamp(above >= gap ? above : target.bottom + gap, viewport.height - size.height),
  };
}

const normalize = text => (text || '').trim().replace(/\s+/g, ' ');

function selectedTextTruncated(target) {
  if (!target.matches('select')) return false;
  const style = target.ownerDocument.defaultView.getComputedStyle(target);
  const canvas = target.ownerDocument.createElement('canvas');
  const context = canvas.getContext('2d'); context.font = style.font;
  const width = context.measureText(target.selectedOptions[0]?.textContent || '').width;
  // Native selects omit clipped option text from scrollWidth; reserve their disclosure arrow.
  return width > target.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) - 20;
}

export function tooltipContent(target) {
  const label = target.dataset.tooltip, shortcut = normalize(target.dataset.tooltipShortcut);
  const copy = target.cloneNode(true);
  copy.querySelectorAll('svg,[aria-hidden="true"],[hidden],.sr-only').forEach(child => child.remove());
  const visible = normalize(target.matches('select') ? target.selectedOptions[0]?.textContent : copy.textContent);
  const labeled = /[\p{L}\p{N}]/u.test(visible);
  const truncated = selectedTextTruncated(target) || [target, ...target.querySelectorAll('*')].some(element => {
    const style = element.ownerDocument.defaultView.getComputedStyle(element);
    return element.getClientRects().length && element.scrollWidth > element.clientWidth &&
      (element === target || style.textOverflow === 'ellipsis');
  });
  if (shortcut && labeled) return { label: '', shortcut };
  if (!label || (!truncated && (target.dataset.tooltipOverflow !== undefined || visible === normalize(label)))) return null;
  return { label, shortcut };
}

export function installTooltips(doc = document) {
  if (installed.has(doc)) return installed.get(doc);
  const win = doc.defaultView;
  const bubble = doc.createElement('div');
  bubble.id = 'workspace-tooltip'; bubble.className = 'workspace-tooltip';
  bubble.setAttribute('role', 'tooltip'); bubble.hidden = true;
  let active, timer, closeTimer;
  let hovered, bubbleHovered = false;
  const hide = () => {
    win.clearTimeout(timer); timer = undefined;
    win.clearTimeout(closeTimer); closeTimer = undefined;
    if (active) {
      const ids = (active.getAttribute('aria-describedby') || '').split(/\s+/).filter(id => id && id !== bubble.id);
      if (ids.length) active.setAttribute('aria-describedby', ids.join(' ')); else active.removeAttribute('aria-describedby');
    }
    active = null; bubbleHovered = false; bubble.hidden = true;
  };
  const show = target => {
    if (!target.isConnected || !target.dataset.tooltip) return;
    hide();
    const content = tooltipContent(target);
    if (!content) return;
    active = target; bubble.replaceChildren();
    if (content.label) { const label = doc.createElement('span'); label.textContent = content.label; bubble.append(label); }
    if (content.shortcut) {
      const hint = doc.createElement('kbd'); hint.textContent = content.shortcut; bubble.append(hint);
    }
    // A modal's top layer clips body tooltips; keep the same bubble in its owner's layer.
    (target.closest('dialog[open]') || doc.body).append(bubble);
    bubble.hidden = false; bubble.style.left = '0px'; bubble.style.top = '0px';
    const position = tooltipPosition(target.getBoundingClientRect(), bubble.getBoundingClientRect(), { width: win.innerWidth, height: win.innerHeight });
    bubble.style.left = `${position.left}px`; bubble.style.top = `${position.top}px`;
    const ids = new Set((target.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean));
    ids.add(bubble.id); target.setAttribute('aria-describedby', [...ids].join(' '));
  };
  const owner = target => target?.closest?.('[data-tooltip]');
  const cancelClose = () => { win.clearTimeout(closeTimer); closeTimer = undefined; };
  const retained = () => active && (hovered === active || bubbleHovered || owner(doc.activeElement) === active);
  const leave = () => {
    win.clearTimeout(timer); timer = undefined;
    cancelClose();
    if (!retained()) closeTimer = win.setTimeout(() => { if (!retained()) hide(); }, 200);
  };
  const over = event => {
    if (bubble.contains(event.target)) { bubbleHovered = true; cancelClose(); return; }
    const target = owner(event.target);
    hovered = target;
    if (!target || target === owner(event.relatedTarget)) return;
    if (target === active) { if (tooltipContent(target)) cancelClose(); else hide(); return; }
    hide(); timer = win.setTimeout(() => show(target), 400);
  };
  const out = event => {
    if (bubble.contains(event.target)) {
      if (bubble.contains(event.relatedTarget)) return;
      bubbleHovered = false; hovered = owner(event.relatedTarget); leave(); return;
    }
    const target = owner(event.target);
    if (!target || target === owner(event.relatedTarget)) return;
    hovered = owner(event.relatedTarget); bubbleHovered = bubble.contains(event.relatedTarget);
    leave();
  };
  const focus = event => { const target = owner(event.target); if (target?.matches(':focus-visible')) show(target); };
  const blur = () => { cancelClose(); closeTimer = win.setTimeout(() => { if (!retained()) hide(); }, 200); };
  const key = event => { if (event.key === 'Escape') hide(); };
  const listeners = [['mouseover', over], ['mouseout', out], ['focusin', focus], ['focusout', blur], ['keydown', key], ['scroll', hide], ['click', hide]];
  for (const [type, handler] of listeners) doc.addEventListener(type, handler, true);
  win.addEventListener('resize', hide);
  const dispose = () => {
    hide(); bubble.remove();
    for (const [type, handler] of listeners) doc.removeEventListener(type, handler, true);
    win.removeEventListener('resize', hide); installed.delete(doc);
  };
  installed.set(doc, dispose);
  return dispose;
}
