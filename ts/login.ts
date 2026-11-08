export {};
import { loadAnalytics } from "./shared/analytics.js";

type SiteConfig = {
  oidc_enabled?: boolean;
  demo_enabled?: boolean;
  demo_username?: string;
  demo_password?: string;
};

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

loadAnalytics();
fetch('/api/config').then(function (r) { return r.json(); }).then(function (cfg: SiteConfig) {
  if (cfg.oidc_enabled) {
    var form = document.getElementById('login-form');
    var btn = document.createElement('a');
    btn.href = '/api/auth/oidc/login';
    btn.className = 'btn btn-outline-primary w-100 mt-2';
    btn.innerHTML = '<i class="fa-solid fa-shield-halved me-2" aria-hidden="true"></i> Login with Authelia';
    if (form && form.parentElement) {
      form.parentElement.insertBefore(btn, form.nextSibling);
    }
  }
  if (cfg.demo_enabled) {
    var demoBlock = document.getElementById('demo-login');
    var demoButton = document.getElementById('demo-login-btn');
    var demoUsernameEl = document.getElementById('demo-username');
    var demoPasswordEl = document.getElementById('demo-password');

    if (demoUsernameEl) demoUsernameEl.textContent = cfg.demo_username || '';
    if (demoPasswordEl) demoPasswordEl.textContent = cfg.demo_password || '';
    if (demoBlock) demoBlock.classList.remove('d-none');

    demoButton?.addEventListener('click', function () {
      var loginForm = document.getElementById('login-form') as HTMLFormElement | null;
      if (!loginForm) return;

      var usernameInput = loginForm.elements.namedItem('username') as HTMLInputElement | null;
      var passwordInput = loginForm.elements.namedItem('password') as HTMLInputElement | null;
      if (usernameInput) usernameInput.value = cfg.demo_username || '';
      if (passwordInput) passwordInput.value = cfg.demo_password || '';

      // Reuse the existing submit handler rather than posting a second time.
      if (typeof loginForm.requestSubmit === 'function') {
        loginForm.requestSubmit();
      } else {
        loginForm.submit();
      }
    });
  }
}).catch(function () { });
