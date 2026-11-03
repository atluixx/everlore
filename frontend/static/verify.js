const form = document.getElementById('verification-form');
const status = document.getElementById('verification-status');
fetch(form.action || window.location.href, {
  method: 'POST',
  body: new URLSearchParams(new FormData(form)),
  credentials: 'same-origin',
}).then(async (response) => {
  const result = await response.json();
  status.textContent = response.ok ? 'Your email is verified. You can now log in.' : (result.error || 'Could not verify your email.');
}).catch(() => {
  status.textContent = 'Could not reach Everlore. Please refresh this page and try again.';
});
