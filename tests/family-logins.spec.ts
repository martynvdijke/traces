import { test, expect } from '@playwright/test';

// Verifies the second-login flow from the family-logins change:
// an admin-created family account can log in, see all shared data,
// and create/edit events (with automatic attribution).
test.describe('Family Logins', () => {
  test.describe.configure({ mode: 'serial' });

  let adminCookie: string;
  let adminCsrf: string;
  let familyCookie: string;
  let familyCsrf: string;
  let familyUserId = 0;
  let eventId = 0;

  const USERNAME = 'e2e-family';
  const PASSWORD = 'family-pass-123';
  const DISPLAY_NAME = 'E2E Family Member';
  const EVENT_TITLE = 'Family Login E2E Event';

  test.beforeAll(async ({ request }) => {
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true }
      });
      expect(setupRes.ok()).toBeTruthy();
      const cookies = setupRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) adminCookie = match[1];
      }
    } else {
      const loginRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123' }
      });
      expect(loginRes.ok()).toBeTruthy();
      const cookies = loginRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) adminCookie = match[1];
      }
    }
    expect(adminCookie).toBeTruthy();

    const csrfResp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${adminCookie}` }
    });
    adminCsrf = (await csrfResp.json()).token;
    expect(adminCsrf).toBeTruthy();
  });

  async function apiAdmin(request: any, url: string, data: any) {
    return request.post(url, {
      headers: { Cookie: `session=${adminCookie}`, 'X-CSRF-Token': adminCsrf },
      data
    });
  }

  async function apiFamily(request: any, url: string, data: any) {
    return request.post(url, {
      headers: { Cookie: `session=${familyCookie}`, 'X-CSRF-Token': familyCsrf },
      data
    });
  }

  test('admin creates a family account with a password', async ({ request }) => {
    const res = await apiAdmin(request, '/api/users', {
      username: USERNAME,
      display_name: DISPLAY_NAME,
      color: '#ef4444',
      password: PASSWORD
    });
    expect(res.ok()).toBeTruthy();
    const created = await res.json();
    familyUserId = created.id ?? created.ID;
    expect(familyUserId).toBeGreaterThan(0);
    // Password must never leak back out of the API.
    expect(JSON.stringify(created)).not.toContain(PASSWORD);
  });

  test('second login authenticates and sees shared data', async ({ request }) => {
    const loginRes = await request.post('/api/login', {
      data: { username: USERNAME, password: PASSWORD }
    });
    expect(loginRes.ok()).toBeTruthy();
    const cookies = loginRes.headers()['set-cookie'];
    const match = cookies ? cookies.match(/session=([^;]+)/) : null;
    expect(match).toBeTruthy();
    familyCookie = match![1];

    const csrfResp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${familyCookie}` }
    });
    familyCsrf = (await csrfResp.json()).token;
    expect(familyCsrf).toBeTruthy();

    const eventsRes = await request.get('/api/events', {
      headers: { Cookie: `session=${familyCookie}` }
    });
    expect(eventsRes.ok()).toBeTruthy();
    expect(Array.isArray(await eventsRes.json())).toBeTruthy();
  });

  test('second login creates and edits shared events with auto-attribution', async ({ request }) => {
    const createRes = await apiFamily(request, '/api/events', { title: EVENT_TITLE });
    expect(createRes.ok()).toBeTruthy();
    const created = await createRes.json();
    eventId = created.id ?? created.ID;
    expect(eventId).toBeGreaterThan(0);
    // Family logins are always stamped as the owner, payload cannot spoof it.
    expect(created.user_id).toBe(familyUserId);

    const editRes = await apiFamily(request, '/api/events', {
      id: eventId,
      title: EVENT_TITLE + ' Edited'
    });
    expect(editRes.ok()).toBeTruthy();

    const listRes = await request.get('/api/events', {
      headers: { Cookie: `session=${familyCookie}` }
    });
    const events = await listRes.json();
    const edited = events.find((e: any) => e.id === eventId);
    expect(edited).toBeTruthy();
    expect(edited.title).toBe(EVENT_TITLE + ' Edited');
    expect(edited.user_id).toBe(familyUserId);
  });

  test('second login sees shared admin UI with attribution', async ({ page }) => {
    await page.context().addCookies([
      { name: 'session', value: familyCookie, url: 'http://localhost:6270' }
    ]);
    await page.goto('/admin.html');
    await expect(page.locator('#persons-tab')).toBeVisible();

    // The family-created event renders with its "added by" attribution once
    // users are loaded (attached, not visible: table may be off-screen tab).
    const row = page.locator('#event-list tr', { hasText: EVENT_TITLE + ' Edited' });
    await row.waitFor({ state: 'attached' });
    await expect(row.locator('.added-by')).toHaveText(DISPLAY_NAME);
  });

  test.afterAll(async ({ request }) => {
    if (eventId) {
      await request.delete(`/api/events?id=${eventId}`, {
        headers: { Cookie: `session=${adminCookie}`, 'X-CSRF-Token': adminCsrf }
      });
    }
    if (familyUserId) {
      await request.delete(`/api/users?id=${familyUserId}`, {
        headers: { Cookie: `session=${adminCookie}`, 'X-CSRF-Token': adminCsrf }
      });
    }
  });
});
