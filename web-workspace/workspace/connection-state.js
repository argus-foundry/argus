export function connectionState(message, state = 'disconnected', doc = document) {
  const icon = doc.getElementById('connection-state');
  icon.textContent = '●';
  icon.dataset.tooltip = message;
  icon.dataset.state = state;
  icon.setAttribute('aria-label', message);
  icon.classList.toggle('connected', state === 'connected');
}
