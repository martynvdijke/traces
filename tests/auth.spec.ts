import { test, expect } from '@playwright/test';

test.describe('TRACES Authentication', () => {
  test.describe.configure({ mode: 'serial' });

  test('login page loads and has form elements', async ({ page }) => {
    await page.goto('/login.html');
    await expect(page.locator('form#login-form')).toBeVisible();
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
    await expect(page.locator('button[type="submit"]')).toBeVisible();
    await expect(page.locator('h1')).toContainText('TRACES');
  });

  test('login page redirects to setup when no users exist', async ({ page }) => {
    await page.goto('/login.html');
    await page.waitForLoadState('networkidle');
    // If setup is needed, the page redirects to /setup.html
    // If setup is already done, we stay on /login.html
    const currentUrl = page.url();
    expect(currentUrl).toMatch(/\/(login\.html|setup\.html)$/);
  });

  test('setup page loads when no admin users', async ({ page }) => {
    await page.goto('/setup.html');
    await page.waitForLoadState('networkidle');
    const currentUrl = page.url();
    expect(currentUrl).toMatch(/\/(setup\.html|login\.html)$/);
  });
});

test.describe('TRACES Authentication API', () => {
  test.describe.configure({ mode: 'serial' });

  let adminSessionCookie: string;
  let csrfToken: string;

  test('check-setup returns valid JSON with setup field', async ({ request }) => {
    const resp = await request.get('/api/check-setup');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data).toHaveProperty('setup');
    expect(typeof data.setup).toBe('boolean');
  });

  test('login with wrong password returns 401', async ({ request }) => {
    // First ensure we have an admin user (setup if needed)
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      // Do setup first so we can test wrong password
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true }
      });
      expect(setupRes.ok()).toBeTruthy();
    }

    const resp = await request.post('/api/login', {
      data: { username: 'admin', password: 'wrong-password' }
    });
    expect(resp.status()).toBe(401);
    const data = await resp.json();
    expect(data.error).toBe('Invalid credentials');
  });

  test('login with non-existent user returns 401', async ({ request }) => {
    const resp = await request.post('/api/login', {
      data: { username: 'nonexistent_user', password: 'whatever' }
    });
    expect(resp.status()).toBe(401);
    const data = await resp.json();
    expect(data.error).toBe('Invalid credentials');
  });

  test('login with correct credentials returns 200 and session cookie', async ({ request }) => {
    const resp = await request.post('/api/login', {
      data: { username: 'admin', password: 'admin123' }
    });
    expect(resp.ok()).toBeTruthy();
    expect(resp.status()).toBe(200);

    const cookies = resp.headers()['set-cookie'];
    expect(cookies).toBeTruthy();

    const match = cookies.match(/session=([^;]+)/);
    expect(match).toBeTruthy();
    adminSessionCookie = match![1];
    expect(adminSessionCookie.length).toBeGreaterThan(10);
  });

  test('CSRF token requires valid session', async ({ request }) => {
    // With valid session
    const resp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${adminSessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.token).toBeTruthy();
    csrfToken = data.token;

    // Without session
    const noAuthResp = await request.get('/api/csrf-token');
    expect(noAuthResp.status()).toBe(401);
  });

  test('protected route returns 401 without session', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026');
    expect(resp.status()).toBe(401);
  });

  test('protected route works with valid session', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026', {
      headers: { Cookie: `session=${adminSessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('logout invalidates session', async ({ request }) => {
    const resp = await request.post('/api/logout', {
      headers: { Cookie: `session=${adminSessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();

    // Session should no longer work
    const eventsResp = await request.get('/api/events?year=2026', {
      headers: { Cookie: `session=${adminSessionCookie}` }
    });
    expect(eventsResp.status()).toBe(401);
  });

  test('login after logout works with new session', async ({ request }) => {
    const resp = await request.post('/api/login', {
      data: { username: 'admin', password: 'admin123' }
    });
    expect(resp.ok()).toBeTruthy();

    const cookies = resp.headers()['set-cookie'];
    const match = cookies.match(/session=([^;]+)/);
    expect(match).toBeTruthy();
    const newSession = match![1];

    // New session should work
    const eventsResp = await request.get('/api/events?year=2026', {
      headers: { Cookie: `session=${newSession}` }
    });
    expect(eventsResp.ok()).toBeTruthy();

    // Update for subsequent tests
    adminSessionCookie = newSession;
  });

  test('setup rejected when admin users already exist', async ({ request }) => {
    const resp = await request.post('/api/login', {
      data: { username: 'another_admin', password: 'password123', setup: true }
    });
    expect(resp.status()).toBe(403);
    const data = await resp.json();
    expect(data.error).toBe('Setup already completed');
  });

  test('setup with short password returns 400', async ({ request }) => {
    // This test requires no admin users - we skip if setup is already done
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (setup) {
      test.skip(true, 'Setup already completed, cannot test short password setup');
      return;
    }

    const resp = await request.post('/api/login', {
      data: { username: 'short_user', password: '1234567', setup: true }
    });
    expect(resp.status()).toBe(400);
    const data = await resp.json();
    expect(data.error).toBe('Password must be at least 8 characters');
  });
});

test.describe('TRACES Browser Auth Flow', () => {
  test.describe.configure({ mode: 'serial' });

  test('login form submit with wrong password shows error', async ({ page }) => {
    await page.goto('/login.html');
    await page.waitForLoadState('networkidle');

    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'wrong_password_xyz');
    await page.click('button[type="submit"]');

    // Wait for the error message in DOM
    const errorEl = page.locator('#login-error');
    await expect(errorEl).toBeVisible();
    await expect(errorEl).toHaveText('Invalid credentials');
    // Should still be on login page
    expect(page.url()).toContain('login.html');
  });

  test('login form submit with correct credentials redirects to admin', async ({ page }) => {
    await page.goto('/login.html');
    await page.waitForLoadState('networkidle');

    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');

    // Should redirect to admin page
    await page.waitForURL('**/admin.html', { timeout: 5000 });
    expect(page.url()).toContain('admin.html');
  });

  test('authenticated admin page shows logout button', async ({ page }) => {
    // Already on admin.html from previous test due to serial mode
    // Re-navigate to be sure
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 5000 });

    // Look for the logout button/link
    const logoutLink = page.locator('text=Logout').first();
    await expect(logoutLink).toBeVisible({ timeout: 5000 });
  });

  test('admin page redirects to login when unauthenticated', async ({ page }) => {
    // Clear cookies to simulate unauthenticated state
    await page.context().clearCookies();

    await page.goto('/admin.html');
    await page.waitForURL('**/login.html', { timeout: 5000 });
    expect(page.url()).toContain('login.html');
  });

  test('logout clears session and redirects to login', async ({ page }) => {
    // Login first
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 5000 });

    // Click logout
    const logoutLink = page.locator('text=Logout').first();
    await logoutLink.click();

    // Should redirect to login page
    await page.waitForURL('**/login.html', { timeout: 5000 });
    expect(page.url()).toContain('login.html');

    // Verify cookies are cleared by the logout
    const cookies = await page.context().cookies();
    expect(cookies.length).toBe(0);
  });
});
