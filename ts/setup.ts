export {};

function showSetupError(msg: string) {
  const el = document.getElementById('setup-error');
  if (el) { el.textContent = msg; el.classList.remove('d-none'); }
}

document.getElementById('setup-form')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const errEl = document.getElementById('setup-error');
  if (errEl) errEl.classList.add('d-none');
  const formData = new FormData(e.target as HTMLFormElement);
  if (formData.get('password') !== formData.get('confirm_password')) {
    showSetupError('Passwords do not match');
    return;
  }
  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: formData.get('username'),
        password: formData.get('password'),
        setup: true
      })
    });
    if (res.ok) {
      window.location.href = '/admin.html';
    } else {
      const data = await res.json();
      showSetupError(data.error || 'Setup failed');
    }
  } catch (err) {
    showSetupError('Setup failed: ' + (err as Error).message);
  }
});

fetch('/api/config').then(function (r) { return r.json(); }).then(function (cfg) {
  if (cfg.umami_url && cfg.umami_site) {
    var s = document.createElement('script');
    s.async = true;
    s.defer = true;
    s.src = cfg.umami_url + '/script.js';
    s.setAttribute('data-website-id', cfg.umami_site);
    document.head.appendChild(s);
  }
}).catch(function () { });
