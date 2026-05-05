import { test, expect } from '@playwright/test';

test.describe('TRACES Login Page', () => {
  test('should have login form', async ({ page }) => {
    await page.goto('/login.html');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });
});

test.describe('TRACES Admin Backend', () => {
  test.describe.configure({ mode: 'serial' });

  let sessionCookie: string;
  let csrfToken: string;

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
        if (match) sessionCookie = match[1];
      }
    } else {
      const loginRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123' }
      });
      expect(loginRes.ok()).toBeTruthy();
      const cookies = loginRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) sessionCookie = match[1];
      }
    }

    expect(sessionCookie).toBeTruthy();

    const csrfResp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const csrfData = await csrfResp.json();
    csrfToken = csrfData.token;
    expect(csrfToken).toBeTruthy();
  });

  test('should create a new event via POST /api/events', async ({ request }) => {
    const resp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        title: 'E2E Test Event',
        description: 'Created by Playwright test',
        date: '2026-05-03',
        location: 'Test City',
        media_type: 'image',
        tags: 'test,e2e',
        sort_order: 0,
        is_public: false,
        latitude: 40.7128,
        longitude: -74.0060
      }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBeGreaterThan(0);
    expect(data.title).toBe('E2E Test Event');
    expect(data.date).toBe('2026-05-03');
    expect(data.latitude).toBe(40.7128);
    expect(data.longitude).toBe(-74.0060);
  });

  test('should return created event in events list', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=10', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    const testEvent = data.find((e: any) => e.title === 'E2E Test Event');
    expect(testEvent).toBeDefined();
    expect(testEvent.date).toBe('2026-05-03');
  });

  test('should respect limit and sort params on /api/events', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=3', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    expect(data.length).toBeLessThanOrEqual(3);
  });

  test('should create a person via POST /api/persons', async ({ request }) => {
    const resp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        name: 'E2E Test Person',
        bio: 'A test person created by Playwright',
        color: '#ef4444'
      }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBeGreaterThan(0);
    expect(data.name).toBe('E2E Test Person');
  });

  test('should return persons list', async ({ request }) => {
    const resp = await request.get('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    const testPerson = data.find((p: any) => p.name === 'E2E Test Person');
    expect(testPerson).toBeDefined();
  });

  test('should reject unauthenticated event creation', async ({ request }) => {
    const resp = await request.post('/api/events', {
      data: { title: 'Should fail' }
    });
    expect(resp.status()).toBe(401);
  });

  test('should create event through browser UI form', async ({ page }) => {
    // Collect console errors for debugging
    const errors: string[] = [];
    page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()); });

    // Login via the browser UI
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 10000 });
    await page.waitForTimeout(1000);

    // Verify admin page loaded
    await expect(page.locator('#event-list')).toBeVisible({ timeout: 5000 });

    // Open modal directly via JS to avoid onclick timing issues
    await page.evaluate(() => (window as any).openEventModal());
    await page.waitForSelector('#event-title', { state: 'visible', timeout: 5000 });

    // Verify modal opened: event-title should be visible
    if (!await page.locator('#event-title').isVisible()) {
      console.log('Console errors:', errors);
      throw new Error('Event modal did not open. Console errors: ' + errors.join(', '));
    }

    // Fill in the event form
    await page.fill('#event-title', 'Browser UI Test Event');
    await page.fill('#event-desc', 'Created via browser form');
    await page.fill('#event-date', '2026-05-04');
    await page.fill('#event-location', 'Browser City');

    // Click "Save Event"
    await page.click('#event-form button[type="submit"]');

    // Wait for the event to appear in the list (indicates successful creation)
    await expect(page.locator('#event-list')).toContainText('Browser UI Test Event', { timeout: 10000 });

    // Delete the test event via API to clean up
    const sessionCookie = (await page.context().cookies()).find(c => c.name === 'session');
    if (sessionCookie) {
      const request = page.request;
      const csrfResp = await request.get('/api/csrf-token', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const { token } = await csrfResp.json();

      const listResp = await request.get('/api/events?year=2026&sort=desc&limit=20', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const events = await listResp.json();
      const testEvent = events.find((e: any) => e.title === 'Browser UI Test Event');
      if (testEvent) {
        await request.delete(`/api/events?id=${testEvent.id}`, {
          headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token }
        });
      }
    }
  });

  test('should delete created test event', async ({ request }) => {
    const listResp = await request.get('/api/events?year=2026&sort=desc&limit=10', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const events = await listResp.json();
    const testEvent = events.find((e: any) => e.title === 'E2E Test Event');
    if (testEvent) {
      const resp = await request.delete(`/api/events?id=${testEvent.id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });

  test('should delete created test person', async ({ request }) => {
    const listResp = await request.get('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const persons = await listResp.json();
    const testPerson = persons.find((p: any) => p.name === 'E2E Test Person');
    if (testPerson) {
      const resp = await request.delete(`/api/persons?id=${testPerson.id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });
});
