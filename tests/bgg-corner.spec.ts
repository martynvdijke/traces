import { test, expect } from '@playwright/test';

test.describe('BGG boardgame timeline integration', () => {
  test.describe.configure({ mode: 'serial' });

  let sessionCookie: string;
  let csrfToken: string;
  const createdIds: number[] = [];

  const BGG_GAME = 'Catan';
  const BGG_TITLE = 'Played Catan';

  test.beforeAll(async ({ request }) => {
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true },
      });
      expect(setupRes.ok()).toBeTruthy();
      const cookies = setupRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) sessionCookie = match[1];
      }
    } else {
      const loginRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123' },
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
      headers: { Cookie: `session=${sessionCookie}` },
    });
    csrfToken = (await csrfResp.json()).token;
    expect(csrfToken).toBeTruthy();
  });

  async function apiPostWithRequest(request: any, url: string, body: any, extra: Record<string, string> = {}) {
    return request.post(url, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken, ...extra },
      data: body,
    });
  }

  async function apiGetWithRequest(request: any, url: string) {
    return request.get(url, {
      headers: { Cookie: `session=${sessionCookie}` },
    });
  }

  test('seed BGG play appears in public timeline with media_type boardgame', async ({ request }) => {
    // Seed via guarded endpoint (requires E2E_BGG_SEED / CI / PLAYWRIGHT env on server)
    const seedRes = await apiPostWithRequest(request, '/api/bgg/seed', {
      game: BGG_GAME,
      date: '2024-06-15',
      location: 'Test Table',
    });
    if (!seedRes.ok()) {
      test.skip(true, `BGG seed endpoint unavailable (status ${seedRes.status()}) – requires E2E_BGG_SEED/CI/PLAYWRIGHT on server`);
      return;
    }
    const data = await seedRes.json();
    const seededId = data.id as number;
    expect(seededId).toBeGreaterThan(0);
    createdIds.push(seededId);

    // Verify via GET /api/events
    const eventsRes = await apiGetWithRequest(request, '/api/events');
    expect(eventsRes.ok()).toBeTruthy();
    const events: any[] = await eventsRes.json();
    const foundInEvents = events.find((e: any) => e.id === seededId || e.title === BGG_TITLE);
    expect(foundInEvents, 'seeded BGG play should appear in /api/events').toBeTruthy();
    expect(foundInEvents.media_type).toBe('boardgame');

    // Verify via GET /api/events/full
    const fullRes = await apiGetWithRequest(request, '/api/events/full');
    expect(fullRes.ok()).toBeTruthy();
    const full: any[] = await fullRes.json();
    const foundInFull = full.find((e: any) => e.id === seededId || e.title === BGG_TITLE);
    expect(foundInFull, 'seeded BGG play should appear in /api/events/full').toBeTruthy();
    expect(foundInFull.media_type).toBe('boardgame');

    // Verify deleted endpoints are gone
    const deletedEvents = await apiGetWithRequest(request, '/api/bgg/events');
    expect(deletedEvents.status()).toBe(404);
    const deletedStats = await apiGetWithRequest(request, '/api/bgg/stats');
    expect(deletedStats.status()).toBe(404);
  });

  test('main-timeline boardgame filter checkbox exists', async ({ page }) => {
    await page.context().addCookies([{ name: 'session', value: sessionCookie, url: `http://localhost:${process.env.E2E_PORT || '6270'}` }]);
    await page.goto('/');
    // The media filter checkboxes live inside a collapsed advanced-filters panel,
    // so assert attachment rather than visibility (same as the existing types).
    const cb = page.locator('#filter-media-boardgame');
    await expect(cb).toBeAttached({ timeout: 5000 });
    await expect(cb).toHaveAttribute('type', 'checkbox');
  });

  test.afterAll(async ({ request }) => {
    if (!csrfToken) return;
    const headers = { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken };
    for (const id of createdIds) {
      if (id) await request.delete(`/api/events?id=${id}`, { headers });
    }
  });
});
