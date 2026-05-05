import { test, expect } from '@playwright/test';

test.describe('TRACES Search', () => {
  test('should have search input', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#search-input')).toBeVisible();
  });
});

test.describe('TRACES Map', () => {
  test('should load map container', async ({ page }) => {
    await page.goto('/');
    await page.locator('#map-tab').click();
    await page.waitForTimeout(500);
    await expect(page.locator('#map-container')).toBeVisible();
  });

  test('should have map container or placeholder visible', async ({ page }) => {
    await page.goto('/');
    await page.locator('#map-tab').click();
    await page.waitForTimeout(1000);
    const mapContainer = page.locator('#map-container');
    await expect(mapContainer).toBeVisible();
    const content = await mapContainer.innerHTML();
    expect(content.length).toBeGreaterThan(0);
  });
});

test.describe('TRACES API Features', () => {
  let sessionCookie: string;

  test.beforeAll(async ({ request }) => {
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true }
      });
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

  test('should return persons', async ({ request }) => {
    const resp = await request.get('/api/persons', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return full events list', async ({ request }) => {
    const resp = await request.get('/api/events/full', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return events sorted desc with limit', async ({ request }) => {
    const resp = await request.get('/api/events?sort=desc&limit=5', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    expect(data.length).toBeLessThanOrEqual(5);
  });

  test('should return stats', async ({ request }) => {
    const resp = await request.get('/api/stats?year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.total).toBeDefined();
  });
});
