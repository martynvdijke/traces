import { test, expect } from '@playwright/test';

async function getAdminSession(request: any): Promise<{ cookie: string; csrf: string }> {
  const setupResp = await request.get('/api/check-setup');
  const { setup } = await setupResp.json();

  const loginResp = await request.post('/api/login', {
    data: setup
      ? { username: 'admin', password: 'admin123' }
      : { username: 'admin', password: 'admin123', setup: true }
  });
  expect(loginResp.ok()).toBeTruthy();

  const cookies: string = loginResp.headers()['set-cookie'] || '';
  const match = cookies.match(/session=([^;]+)/);
  expect(match).toBeTruthy();
  const cookie = match![1];

  const csrfResp = await request.get('/api/csrf-token', {
    headers: { Cookie: `session=${cookie}` }
  });
  const csrfData = await csrfResp.json();
  expect(csrfData.token).toBeTruthy();

  return { cookie, csrf: csrfData.token as string };
}

const stamp = Date.now();
const capsuleTitle = 'Sealed capsule ' + stamp;
const capsuleBody = 'Capsule body ' + stamp + ' must never reach the page';
const revealAt = new Date(Date.now() + 5 * 24 * 60 * 60 * 1000)
  .toISOString()
  .replace(/\.\d{3}Z$/, 'Z');

test.describe('Time capsules', () => {
  test.describe.configure({ mode: 'serial' });

  let cookie = '';
  let csrf = '';
  let capsuleId = 0;

  const headers = (): Record<string, string> => ({
    Cookie: `session=${cookie}`,
    'X-CSRF-Token': csrf
  });

  test.beforeAll(async ({ request }) => {
    const session = await getAdminSession(request);
    cookie = session.cookie;
    csrf = session.csrf;
  });

  test('creates an event with a future reveal date', async ({ request }) => {
    const resp = await request.post('/api/events', {
      headers: headers(),
      data: {
        title: capsuleTitle,
        description: capsuleBody,
        date: '2026-03-14',
        location: 'Secret place',
        media_type: 'image',
        tags: 'capsule,e2e',
        is_public: false,
        reveal_at: revealAt
      }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBeGreaterThan(0);
    expect(data.reveal_at).toBe(revealAt);
    capsuleId = data.id;
  });

  test('sealed event is absent from the events list', async ({ request }) => {
    const resp = await request.get('/api/events/full', { headers: headers() });
    expect(resp.ok()).toBeTruthy();
    const events = await resp.json();
    expect(Array.isArray(events)).toBeTruthy();
    expect(events.some((e: any) => e.id === capsuleId)).toBeFalsy();
  });

  test('sealed event is listed as a capsule without its body', async ({ request }) => {
    const resp = await request.get('/api/capsules', { headers: headers() });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data.capsules)).toBeTruthy();

    const capsule = data.capsules.find((c: any) => c.id === capsuleId);
    expect(capsule).toBeTruthy();
    expect(capsule.title).toBe(capsuleTitle);
    expect(capsule.media_type).toBe('image');
    expect(capsule.reveal_at).toBe(revealAt);
    expect(capsule.seconds_until_reveal).toBeGreaterThan(0);
    expect(JSON.stringify(capsule)).not.toContain(capsuleBody);
    expect(JSON.stringify(capsule)).not.toContain('Secret place');
  });

  test('capsules tab shows the title and never the body', async ({ page }) => {
    await page.context().addCookies([{
      name: 'session',
      value: cookie,
      domain: 'localhost',
      path: '/'
    }]);
    await page.goto('/');

    await page.locator('#capsules-tab').click();

    const card = page.locator('.capsule-card', { hasText: capsuleTitle });
    await expect(card).toBeVisible();
    await expect(card).toContainText(capsuleTitle);
    await expect(card.locator('img, video, audio')).toHaveCount(0);
    await expect(page.locator('#capsules-list')).not.toContainText(capsuleBody);
    await expect(page.locator('#capsules-count')).toHaveText(/\d+/);
  });

  test('unseal opens the capsule', async ({ request }) => {
    const resp = await request.post('/api/capsules/unseal', {
      headers: headers(),
      data: { id: capsuleId }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBe(capsuleId);
    expect(data.title).toBe(capsuleTitle);
    expect(data.revealed_at).toBeTruthy();
  });

  test('opened capsule appears in the events list and leaves the capsule list', async ({ request }) => {
    const eventsResp = await request.get('/api/events/full', { headers: headers() });
    expect(eventsResp.ok()).toBeTruthy();
    const events = await eventsResp.json();
    const opened = events.find((e: any) => e.id === capsuleId);
    expect(opened).toBeTruthy();
    expect(opened.reveal_at).toBe('');
    expect(opened.revealed_at).toBeTruthy();

    const capsulesResp = await request.get('/api/capsules', { headers: headers() });
    const data = await capsulesResp.json();
    expect(data.capsules.some((c: any) => c.id === capsuleId)).toBeFalsy();
  });
});
