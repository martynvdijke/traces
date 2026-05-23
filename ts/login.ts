export {};

function showLoginError(msg: string) {
  const el = document.getElementById('login-error');
  if (el) { el.textContent = msg; el.classList.remove('d-none'); }
}

document.getElementById('login-form')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const errEl = document.getElementById('login-error');
  if (errEl) errEl.classList.add('d-none');
  const formData = new FormData(e.target as HTMLFormElement);
  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: formData.get('username'),
        password: formData.get('password')
      })
    });
    if (res.ok) {
      window.location.href = '/admin.html';
    } else {
      const data = await res.json();
      showLoginError(data.error || 'Invalid credentials');
    }
  } catch (err) {
    showLoginError('Login failed: ' + (err as Error).message);
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
