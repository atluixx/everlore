document.querySelectorAll('[data-api-form]').forEach((form) => {
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = form.querySelector('button[type="submit"], button:not([type])');
    const feedback = form.querySelector('.form-feedback');
    button.disabled = true;
    feedback.textContent = 'Sending…';
    feedback.className = 'form-feedback';
    try {
      const response = await fetch(form.action, {method: 'POST', body: new URLSearchParams(new FormData(form)), credentials: 'same-origin'});
      const data = await response.json();
      feedback.textContent = data.message || data.error || 'Something went wrong. Please try again.';
      feedback.classList.add(response.ok ? 'message' : 'error');
      if (response.ok && form.dataset.redirectOnSuccess) { window.location.assign(form.dataset.redirectOnSuccess); return; }
      if (response.ok && form.hasAttribute('data-reload-on-success')) { window.location.reload(); return; }
      if (response.ok) form.querySelectorAll('input:not([type="hidden"])').forEach((input) => { input.value = ''; });
    } catch (_) {
      feedback.textContent = 'Could not reach the archive. Check your connection and try again.';
      feedback.classList.add('error');
    } finally { button.disabled = false; }
  });
});
document.querySelectorAll('[data-reading-toggle]').forEach((button) => {
  const kind = button.dataset.readingToggle;
  const target = kind === 'night' ? document.body : document.querySelector('.story');
  const key = `everlore-reading-${kind}`;
  let enabled = false;
  try { enabled = localStorage.getItem(key) === 'true'; } catch (_) { /* Storage may be unavailable. */ }
  target.classList.toggle(kind, enabled);
  button.setAttribute('aria-pressed', String(enabled));
  button.addEventListener('click', () => {
    const active = target.classList.toggle(kind);
    button.setAttribute('aria-pressed', String(active));
    try { localStorage.setItem(key, String(active)); } catch (_) { /* Keep the current page usable. */ }
  });
});
// Coalesce rapid repeated searches without delaying any other action.
document.querySelectorAll('[data-search-form]').forEach((form) => {
  let pending;
  let submitting = false;
  form.addEventListener('submit', (event) => {
    if (submitting) return;
    event.preventDefault();
    clearTimeout(pending);
    pending = setTimeout(() => {
      submitting = true;
      form.requestSubmit();
    }, 300);
  });
});
