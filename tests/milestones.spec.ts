import { test, expect } from '@playwright/test';

test.describe('Milestones View', () => {
  test.describe.configure({ mode: 'serial' });

  let sessionCookie: string;
  let csrfToken: string;
  let personId: number;
  const createdEventIds: number[] = [];
  const PERSON_NAME = 'Mila Milestone E2E';

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
    csrfToken = (await csrfResp.json()).token;
    expect(csrfToken).toBeTruthy();
  });

  async function apiPost(request: any, url: string, data: any) {
    return request.post(url, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data
    });
  }

  test('person page renders linked events grouped by year-of-life with ages', async ({ page, request }) => {
    // Seed: person with birth date + two linked events in different years of life.
    const personResp = await apiPost(request, '/api/persons', {
      name: PERSON_NAME,
      bio: 'E2E milestone child',
      birth_date: '2022-03-15',
      color: '#7c3aed'
    });
    expect(personResp.ok()).toBeTruthy();
    const person = await personResp.json();
    personId = person.id;
    expect(personId).toBeTruthy();

    const events = [
      { title: 'Milestone First Steps', date: '2022-12-01' }, // age 8 months -> Year 1
      { title: 'Milestone Second Birthday Party', date: '2024-03-20' } // age 2y 0m -> Year 3
    ];
    for (const ev of events) {
      const evResp = await apiPost(request, '/api/events', {
        title: ev.title,
        description: 'Created by milestones e2e',
        date: ev.date,
        location: 'Test City',
        person_id: personId,
        media_type: 'image'
      });
      expect(evResp.ok()).toBeTruthy();
      const created = await evResp.json();
      createdEventIds.push(created.id ?? created.ID);
    }

    // Open admin UI authenticated as the seeded session.
    await page.context().addCookies([
      { name: 'session', value: sessionCookie, url: 'http://localhost:6270' }
    ]);
    await page.goto('/admin.html');
    await page.waitForSelector('#persons-tab', { state: 'visible', timeout: 5000 });

    // Entry point: Persons tab -> card's Milestones button.
    await page.click('#persons-tab');
    const card = page.locator('.person-card', { hasText: PERSON_NAME });
    await expect(card).toBeVisible({ timeout: 5000 });
    // dispatchEvent still exercises the button's inline onclick handler
    // (trusted-input synthesis is unreliable in some headless environments).
    await card.locator('button', { hasText: 'Milestones' }).dispatchEvent('click');

    // Milestone pane renders groups + ages.
    const pane = page.locator('#milestones-pane');
    await expect(pane).toBeVisible();
    await expect(page.locator('#milestones-header')).toContainText(PERSON_NAME);
    await expect(page.locator('#milestones-header')).toContainText('2022-03-15');

    const year1 = page.locator('.milestone-group', { hasText: 'Year 1' });
    await expect(year1).toBeVisible();
    await expect(year1).toContainText('Milestone First Steps');
    await expect(year1.locator('.age-badge')).toContainText('8 months');

    const year3 = page.locator('.milestone-group', { hasText: 'Year 3' });
    await expect(year3).toBeVisible();
    await expect(year3).toContainText('Milestone Second Birthday Party');
    await expect(year3.locator('.age-badge')).toContainText('2 years');
  });

  test.afterAll(async ({ request }) => {
    if (!csrfToken) return;
    for (const id of createdEventIds) {
      if (id) await request.delete(`/api/events?id=${id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
    }
    if (personId) {
      await request.delete(`/api/persons?id=${personId}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
    }
  });
});
