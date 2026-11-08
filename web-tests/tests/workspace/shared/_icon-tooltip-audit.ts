import assert from 'node:assert/strict';

// Usable for any app container; labels must live on the control, not its hidden SVG.
export async function auditIconTooltips(page, containerSelector) {
  const failures = await page.locator(containerSelector).evaluate(container => {
    const indicators = '.task-status-icon,.task-pin,.task-role-tag,.project-caret,.connection-state';
    const missing = [...container.querySelectorAll(`button,a,[role="button"],summary,${indicators}`)].filter(element => {
      if (!element.getClientRects().length) return false;
      if (element.matches(indicators)) return true;
      if (element.matches('.task-disclosure')) return true;
      const copy = element.cloneNode(true);
      copy.querySelectorAll('svg,[aria-hidden="true"],[hidden],.sr-only').forEach(icon => icon.remove());
      return !/[\p{L}\p{N}]/u.test(copy.textContent.trim());
    }).flatMap(element => {
      const label = element.id || element.className.baseVal || element.className || element.tagName;
      return !element.getAttribute('data-tooltip')?.trim() || !element.getAttribute('aria-label')?.trim() || element.closest('[title]') ? [String(label)] : [];
    });
    const redundant = [...container.querySelectorAll('[data-tooltip]')].filter(element => {
      if (!element.getClientRects().length || element.dataset.tooltipShortcut) return false;
      const copy = element.cloneNode(true);
      copy.querySelectorAll('svg,[aria-hidden="true"],[hidden],.sr-only').forEach(child => child.remove());
      const normalize = text => (text || '').trim().replace(/\s+/g, ' ');
      const text = normalize(element.matches('select') ? element.selectedOptions[0]?.textContent : copy.textContent);
      const clipped = [element, ...element.querySelectorAll('*')].some(child => child.getClientRects().length && child.scrollWidth > child.clientWidth && (child === element || getComputedStyle(child).textOverflow === 'ellipsis'));
      return !clipped && element.dataset.tooltipOverflow === undefined && text === normalize(element.dataset.tooltip);
    }).map(element => `Redundant tooltip on ${element.id || element.className || element.tagName}`);
    return [...missing, ...redundant];
  });
  assert.deepEqual(failures, [], `Icon controls need data-tooltip and aria-label without title; labeled tooltips must add information in ${containerSelector}`);
}
