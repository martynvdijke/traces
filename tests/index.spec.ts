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

  test('should show loaded version in footer', async ({ page }) => {
    await page.goto('/');
    const versionEl = page.locator('#version-display');
    await expect(versionEl).toBeVisible();
    // Wait for version to actually load (not the placeholder text)
    await expect(versionEl).not.toHaveText('Version loading...', { timeout: 10000 });
    const text = await versionEl.textContent();
    expect(text).toMatch(/^v\d+\.\d+\.\d+/);
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
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.version).toBeDefined();
    expect(data.version).toMatch(/^\d+\.\d+\.\d+/);
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

test.describe('TRACES Search & Discovery', () => {
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

  test('should have global search input', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#search-input')).toBeVisible();
    await expect(page.locator('#search-input')).toHaveAttribute('placeholder', /search/i);
  });

  test('should search events via API', async ({ request }) => {
    const resp = await request.get('/api/events/search?q=test&year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should global search via API', async ({ request }) => {
    const resp = await request.get('/api/events/search/global?q=test&limit=5', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return stats distribution', async ({ request }) => {
    const resp = await request.get('/api/stats/distribution?year=2026', {
      headers: sessionCookie ? { Cookie: `session=${sessionCookie}` } : {}
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.event_count).toBeDefined();
    expect(data.by_month).toBeDefined();
    expect(data.by_weekday).toBeDefined();
    expect(Array.isArray(data.by_tag)).toBeTruthy();
    expect(Array.isArray(data.by_person)).toBeTruthy();
    expect(Array.isArray(data.by_user)).toBeTruthy();
    expect(Array.isArray(data.by_location)).toBeTruthy();
  });

  test('should have filters toggle button', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('button[title="Advanced Filters"]')).toBeVisible();
  });

  test('should toggle filters panel', async ({ page }) => {
    await page.goto('/');
    await page.locator('button[title="Advanced Filters"]').click();
    await expect(page.locator('#advanced-filters')).toBeVisible();
  });

  test('should have Stats tab', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#stats-tab')).toBeVisible();
    await expect(page.locator('#stats-tab')).toContainText('Stats');
  });

  test('should load stats distribution when clicking Stats tab', async ({ page }) => {
    await page.goto('/');
    await page.locator('#stats-tab').click();
    await page.waitForTimeout(2000);
    await expect(page.locator('#stats-distribution-container')).toBeVisible();
    // Should not show the loading text anymore after load
    await expect(page.locator('#stats-distribution-container')).not.toContainText('Loading statistics...');
  });
});

test.describe('E-Ink Mode', () => {
  test('should have e-ink toggle button on main page', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#eink-toggle')).toBeVisible();
    await expect(page.locator('#eink-toggle')).toHaveAttribute('aria-label', 'Toggle E-Ink Mode');
  });

  test('should activate e-ink mode when button clicked', async ({ page }) => {
    await page.goto('/');
    await page.locator('#eink-toggle').click();
    const hasClass = await page.evaluate(() =>
      document.documentElement.classList.contains('eink-mode')
    );
    expect(hasClass).toBeTruthy();
  });

  test('should deactivate e-ink mode when button clicked twice', async ({ page }) => {
    await page.goto('/');
    await page.locator('#eink-toggle').click();
    await page.locator('#eink-toggle').click();
    const hasClass = await page.evaluate(() =>
      document.documentElement.classList.contains('eink-mode')
    );
    expect(hasClass).toBeFalsy();
  });

  test('should activate via URL parameter eink=1', async ({ page }) => {
    await page.goto('/?eink=1');
    await page.waitForTimeout(300);
    const hasClass = await page.evaluate(() =>
      document.documentElement.classList.contains('eink-mode')
    );
    expect(hasClass).toBeTruthy();
  });

  test('should deactivate via URL parameter eink=0', async ({ page }) => {
    await page.goto('/?eink=1');
    await page.waitForTimeout(300);
    await page.goto('/?eink=0');
    await page.waitForTimeout(300);
    const hasClass = await page.evaluate(() =>
      document.documentElement.classList.contains('eink-mode')
    );
    expect(hasClass).toBeFalsy();
  });

  test('should activate via E keyboard shortcut', async ({ page }) => {
    await page.goto('/');
    await page.keyboard.press('E');
    await page.waitForTimeout(100);
    const hasClass = await page.evaluate(() =>
      document.documentElement.classList.contains('eink-mode')
    );
    expect(hasClass).toBeTruthy();
  });

  test('should load eink.css when e-ink mode activated', async ({ page }) => {
    await page.goto('/?eink=1');
    await page.waitForTimeout(300);
    const hasLink = await page.evaluate(() =>
      !!document.querySelector('link[href*="eink.css"]')
    );
    expect(hasLink).toBeTruthy();
  });
});
