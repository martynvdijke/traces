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

  test.beforeAll(async ({ request }) => {
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true }
      });
      const setupData = await setupRes.json();
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
      if (loginRes.ok()) {
        const cookies = loginRes.headers()['set-cookie'];
        if (cookies) {
          const match = cookies.match(/session=([^;]+)/);
          if (match) sessionCookie = match[1];
        }
      }
    }
  });

  test('should create a new event via POST /api/events', async ({ request }) => {
    const resp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}` },
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
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=10');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    const testEvent = data.find((e: any) => e.title === 'E2E Test Event');
    expect(testEvent).toBeDefined();
    expect(testEvent.date).toBe('2026-05-03');
  });

  test('should respect limit and sort params on /api/events', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=3');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    expect(data.length).toBeLessThanOrEqual(3);
  });

  test('should create a person via POST /api/persons', async ({ request }) => {
    const resp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}` },
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
    const resp = await request.get('/api/persons');
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

  test('should delete created test event', async ({ request }) => {
    const listResp = await request.get('/api/events?year=2026&sort=desc&limit=10');
    const events = await listResp.json();
    const testEvent = events.find((e: any) => e.title === 'E2E Test Event');
    if (testEvent) {
      const resp = await request.delete(`/api/events?id=${testEvent.id}`, {
        headers: { Cookie: `session=${sessionCookie}` }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });

  test('should delete created test person', async ({ request }) => {
    const listResp = await request.get('/api/persons');
    const persons = await listResp.json();
    const testPerson = persons.find((p: any) => p.name === 'E2E Test Person');
    if (testPerson) {
      const resp = await request.delete(`/api/persons?id=${testPerson.id}`, {
        headers: { Cookie: `session=${sessionCookie}` }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });
});