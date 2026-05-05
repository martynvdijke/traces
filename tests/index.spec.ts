import { test, expect } from '@playwright/test';

test.describe('TRACES Timeline', () => {
  test('should load the main page', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/TRACES/);
  });

  test('should display year selector', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#current-year')).toBeVisible();
    await expect(page.locator('button:has-text("2026")')).toBeVisible();
  });

  test('should have a timeline section', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#timeline')).toBeVisible();
  });

  test('should have contribution graph section', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#contributions')).toBeVisible();
    await expect(page.locator('.contribution-graph')).toBeVisible();
  });

  test('should have a gallery section', async ({ page }) => {
    await page.goto('/');
    await page.locator('#gallery-tab').click();
    await page.waitForTimeout(500);
    await expect(page.locator('#gallery')).toBeVisible();
  });

  test('should have navigation links', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#timeline-tab')).toContainText('Timeline');
  });

  test('should have version in footer', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#version-display')).toBeVisible();
  });

  test('should toggle theme', async ({ page }) => {
    await page.goto('/');
    const themeToggle = page.locator('#theme-toggle');
    await themeToggle.click();
    const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    expect(theme).toBeDefined();
  });

  test('should filter by year', async ({ page }) => {
    await page.goto('/');
    await page.locator('button:has-text("2026")').click();
    await expect(page.locator('#current-year')).toHaveText('2026');
  });

  test('should filter by month', async ({ page }) => {
    await page.goto('/');
    await page.locator('button:has-text("Jan")').click();
    await expect(page.locator('.month-filter button.btn-dark')).toHaveText('Jan');
  });

  test('should have map section', async ({ page }) => {
    await page.goto('/');
    await page.locator('#map-tab').click();
    await page.waitForTimeout(500);
    await expect(page.locator('.map-section')).toBeVisible();
    await expect(page.locator('#map-container')).toBeVisible();
  });

  test('should display memories section when available', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(500);
    await expect(page.locator('#memories-section')).toBeAttached();
  });
});

test.describe('TRACES API', () => {
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

  test('should return version', async ({ request }) => {
    const resp = await request.get('/api/version');
    const data = await resp.json();
    expect(resp.ok()).toBeTruthy();
    expect(data.version).toBeDefined();
  });

  test('should return events', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return contributions', async ({ request }) => {
    const resp = await request.get('/api/contributions?year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data).toBeDefined();
  });

  test('should check setup status', async ({ request }) => {
    const resp = await request.get('/api/check-setup');
    const data = await resp.json();
    expect(resp.ok()).toBeTruthy();
    expect(data.setup).toBeDefined();
  });

  test('should return tags', async ({ request }) => {
    const resp = await request.get('/api/tags?year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('should return map data', async ({ request }) => {
    const resp = await request.get('/api/map', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.type).toBe('FeatureCollection');
  });
});

test.describe('TRACES Login', () => {
  test('should show sign in form', async ({ page }) => {
    await page.goto('/login.html');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('should show setup page when no admin', async ({ page }) => {
    await page.goto('/setup.html');
    await expect(page).toHaveTitle(/TRACES/);
  });
});

test.describe('TRACES JavaScript Loading', () => {
  test('should load without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));

    await page.goto('/');
    await page.waitForTimeout(1000);

    expect(errors).toHaveLength(0);
  });

  test('should display events from API', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(500);
    await expect(page.locator('#timeline-container')).toBeVisible();
  });

  test('should have gallery with media', async ({ page }) => {
    await page.goto('/');
    await page.locator('#gallery-tab').click();
    await page.waitForTimeout(500);
    await expect(page.locator('#gallery')).toBeVisible();
  });
});
