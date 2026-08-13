export {};
declare const L: any;

interface TimelineEvent {
  id: number;
  title: string;
  description: string;
  date: string;
  location: string;
  media_type: string;
  media_url: string;
  thumbnail: string;
  media_caption: string;
  tags: string;
  sort_order: number;
  is_public: boolean;
  is_favorite: boolean;
  created_at: string;
  person_id?: number;
  latitude?: number;
  longitude?: number;
  recurring: string;
  weather_data: string;
  start_time: string;
  end_time: string;
  user_id: number;
  person?: {
    id: number;
    name: string;
    avatar_url: string;
    bio: string;
    birth_date: string;
    color: string;
    created_at: string;
  };
  user?: {
    id: number;
    username: string;
    display_name: string;
    color: string;
    avatar_url: string;
  };
}

interface CalendarDay {
  date: string;
  events: TimelineEvent[];
  count: number;
}

interface ContributionMap {
  [date: string]: number;
}

interface Weather {
  temperature: number;
  condition: string;
  icon: string;
  wind_speed: number;
}

type Theme = 'light' | 'dark';

let currentYear: number = new Date().getFullYear();
let currentMonth: number = 0;
// `allEvents` holds the full multi-year story; `events` is the currently visible (filtered) set.
let allEvents: TimelineEvent[] = [];
let events: TimelineEvent[] = [];
let contributions: ContributionMap = {};
let storyChunk: number = 1;
const storyChunkSize: number = 60;
let storyYearObserver: IntersectionObserver | null = null;
let storySentinelObserver: IntersectionObserver | null = null;
let mapInstance: any = null;
let mapMarkers: any[] = [];
let mapPathLine: any = null;
let users: any[] = [];

const monthNames: string[] = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December'
];

const themeToggle = document.getElementById('theme-toggle') as HTMLButtonElement;
const themeIcon = themeToggle?.querySelector('i');

function setTheme(theme: Theme): void {
  document.documentElement.setAttribute('data-theme', theme);
  localStorage.setItem('theme', theme);
  if (themeIcon) {
    themeIcon.className = theme === 'dark' ? 'fa-solid fa-sun' : 'fa-solid fa-moon';
  }
}

function initTheme(): void {
  const savedTheme = (localStorage.getItem('theme') || (
    window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  )) as Theme;
  setTheme(savedTheme);
  themeToggle?.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme') as Theme;
    setTheme(current === 'dark' ? 'light' : 'dark');
  });
}

function changeYear(year: number): void {
  currentYear = year;
  const yearEl = document.getElementById('current-year');
  if (yearEl) yearEl.textContent = String(year);
  document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.remove('btn-primary'));
  document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.add('btn-outline-primary'));
  const btn = document.querySelector(`.year-selector .btn[data-year="${year}"]`);
  if (btn) { btn.classList.remove('btn-outline-primary'); btn.classList.add('btn-primary'); }
  const icsLink = document.getElementById('ics-download') as HTMLAnchorElement;
  if (icsLink) icsLink.href = '/api/events/ics?year=' + year;
  // The story already contains every year — just glide to this one.
  const marker = document.getElementById('year-' + year);
  if (marker) {
    marker.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
  loadStatsDist();
}

function searchEvents(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input?.value) {
    applyAdvancedFilters();
  }
  hideGlobalDropdown();
}

function globalSearchInput(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  const q = input?.value?.trim();
  if (!q || q.length < 2) {
    hideGlobalDropdown();
    return;
  }
  fetch('/api/events/search/global?q=' + encodeURIComponent(q) + '&limit=8')
    .then(r => r.json())
    .then((results: any[]) => {
      const dropdown = document.getElementById('global-search-dropdown');
      if (!dropdown) return;
      if (!Array.isArray(results) || results.length === 0) {
        dropdown.innerHTML = '<div class="global-search-no-results">No matches found</div>';
      } else {
        dropdown.innerHTML = results.map((e: any, i: number) => {
          const year = e.date ? e.date.slice(0, 4) : '';
          const loc = e.location ? ' <span class="search-year"><i class="fa-solid fa-location-dot"></i> ' + escapeHtml(e.location) + '</span>' : '';
          return '<div class="global-search-item" data-id="' + e.id + '" onclick="selectGlobalResult(' + e.id + ')" onmouseenter="highlightGlobalItem(' + i + ')">' +
            '<div class="fw-bold">' + escapeHtml(e.title) + '</div>' +
            '<div class="search-year">' + year + loc + '</div>' +
            '</div>';
        }).join('');
      }
      dropdown.style.display = 'block';
    })
    .catch(() => { hideGlobalDropdown(); });
}

let globalSearchIndex = -1;

function globalSearchKeydown(e: KeyboardEvent): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown || dropdown.style.display === 'none') return;
  const items = dropdown.querySelectorAll('.global-search-item');
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    globalSearchIndex = Math.min(globalSearchIndex + 1, items.length - 1);
    updateGlobalHighlight(items);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    globalSearchIndex = Math.max(globalSearchIndex - 1, 0);
    updateGlobalHighlight(items);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    if (globalSearchIndex >= 0 && items[globalSearchIndex]) {
      const el = items[globalSearchIndex] as HTMLElement;
      selectGlobalResult(parseInt(el.dataset.id || '0'));
    }
  } else if (e.key === 'Escape') {
    hideGlobalDropdown();
  }
}

function updateGlobalHighlight(items: NodeListOf<Element>): void {
  items.forEach((item, i) => {
    item.classList.toggle('active', i === globalSearchIndex);
  });
}

function highlightGlobalItem(index: number): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown) return;
  const items = dropdown.querySelectorAll('.global-search-item');
  items.forEach((item, i) => {
    item.classList.toggle('active', i === index);
  });
  globalSearchIndex = index;
}

function globalSearchFocus(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input?.value?.trim() && input.value.trim().length >= 2) {
    globalSearchInput();
  }
}

function selectGlobalResult(id: number): void {
  hideGlobalDropdown();
  if (id) showMedia(id);
}

function hideGlobalDropdown(): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (dropdown) dropdown.style.display = 'none';
  globalSearchIndex = -1;
}

document.addEventListener('click', (e: Event) => {
  const target = e.target as HTMLElement;
  if (!target.closest('.global-search-wrapper')) {
    hideGlobalDropdown();
  }
});

function filterMonth(month: number): void {
  currentMonth = month;
  const buttons = document.querySelectorAll('.month-filter .btn');
  buttons.forEach((btn, i) => {
    btn.classList.toggle('active', i === month);
    btn.classList.toggle('btn-dark', i === month);
    btn.classList.toggle('btn-outline-dark', i !== month);
  });
  renderStory();
  renderCalendar();
  updateStats();
  renderMapInstance();
}

async function loadContributions(): Promise<void> {
  try {
    const res = await fetch('/api/contributions?year=' + currentYear);
    if (!res.ok) {
      contributions = {};
    } else {
      contributions = await res.json() as ContributionMap;
    }
    renderContributionGraph();
    updateStats();
  } catch (err) {
    console.error('Failed to load contributions:', err);
  }
}

function renderContributionGraph(): void {
  const graph = document.getElementById('contribution-graph');
  if (!graph) return;

  const firstDay = new Date(currentYear, 0, 1);
  const startDay = firstDay.getDay();
  const daysInYear = (currentYear % 4 === 0 && currentYear % 100 !== 0) || currentYear % 400 === 0 ? 366 : 365;
  const maxCount = Math.max(...Object.values(contributions), 1);

  let html = '<div class="graph-months">';
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  months.forEach((m, i) => {
    if (i === 0 || new Date(currentYear, i, 1).getDay() === 0) {
      html += '<span class="month-label">' + m + '</span>';
    }
  });
  html += '</div>';

  html += '<div class="graph-grid"><div class="graph-days">';
  for (let i = 0; i < startDay; i++) {
    html += '<div class="day-cell empty"></div>';
  }

  for (let day = 1; day <= daysInYear; day++) {
    const dateObj = new Date(currentYear, 0, day);
    const dateStr = dateObj.toISOString().split('T')[0];
    const count = contributions[dateStr] || 0;
    const level = count > 0 ? Math.ceil((count / maxCount) * 4) : 0;

    html += '<div class="day-cell level-' + level + '" data-date="' + dateStr + '" data-count="' + count + '" title="' + dateStr + ': ' + count + ' event(s)"></div>';

    if (dateObj.getDay() === 6 && day < daysInYear) {
      html += '</div><div class="graph-days">';
    }
  }
  html += '</div></div>';

  graph.innerHTML = html;
}

function updateStats(): void {
  const eventList = Array.isArray(events) ? events : [];
  const filtered = eventList.length > 0
    ? (currentMonth > 0 ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth) : eventList)
    : [];

  const totalEl = document.getElementById('total-events');
  if (totalEl) totalEl.textContent = filtered.length + ' events';

  const images = filtered.filter(e => e.media_type === 'image').length;
  const videos = filtered.filter(e => e.media_type === 'video').length;
  const audio = filtered.filter(e => e.media_type === 'audio').length;
  const locations = new Set(filtered.map(e => e.location).filter(l => l)).size;

  const statImages = document.getElementById('stat-images');
  const statVideos = document.getElementById('stat-videos');
  const statAudio = document.getElementById('stat-audio');
  const statLocations = document.getElementById('stat-locations');

  if (statImages) statImages.textContent = String(images);
  if (statVideos) statVideos.textContent = String(videos);
  if (statAudio) statAudio.textContent = String(audio);
  if (statLocations) statLocations.textContent = String(locations);
}

let selectedFilterTags: string[] = [];
let filterLocationTimeout: any = null;
let showFavoritesOnly: boolean = false;
let selectedCollectionId: string = '';
let collections: any[] = [];

function toggleFilters(): void {
  const panel = document.getElementById('advanced-filters');
  if (!panel) return;
  const isHidden = panel.style.display === 'none' || !panel.style.display;
  panel.style.display = isHidden ? 'block' : 'none';
}

function applyAdvancedFilters(): void {
  const q = (document.getElementById('search-input') as HTMLInputElement | null)?.value?.trim() || '';
  const personId = (document.getElementById('filter-person') as HTMLSelectElement | null)?.value || '';
  const location = (document.getElementById('filter-location') as HTMLInputElement | null)?.value?.trim() || '';
  const collectionId = (document.getElementById('filter-collection') as HTMLSelectElement | null)?.value || '';
  const mediaTypes: string[] = [];
  if ((document.getElementById('filter-media-image') as HTMLInputElement | null)?.checked) mediaTypes.push('image');
  if ((document.getElementById('filter-media-video') as HTMLInputElement | null)?.checked) mediaTypes.push('video');
  if ((document.getElementById('filter-media-audio') as HTMLInputElement | null)?.checked) mediaTypes.push('audio');

  selectedCollectionId = collectionId;

  const statusEl = document.getElementById('filter-status');
  if (statusEl) statusEl.textContent = 'Filtering...';

  // The story spans all years, so filtering happens client-side over the loaded events.
  const apply = (base: TimelineEvent[]): void => {
    let result = base;
    if (q) {
      const needle = q.toLowerCase();
      result = result.filter(e =>
        (e.title && e.title.toLowerCase().includes(needle)) ||
        (e.description && e.description.toLowerCase().includes(needle)) ||
        (e.location && e.location.toLowerCase().includes(needle)) ||
        (e.tags && e.tags.toLowerCase().includes(needle))
      );
    }
    if (personId) result = result.filter(e => String(e.person_id || '') === personId);
    if (location) result = result.filter(e => e.location && e.location.toLowerCase().includes(location.toLowerCase()));
    if (selectedFilterTags.length > 0) {
      result = result.filter(e => {
        const tags = e.tags ? e.tags.split(',').map(t => t.trim().toLowerCase()) : [];
        return selectedFilterTags.every(t => tags.includes(t.toLowerCase()));
      });
    }
    if (mediaTypes.length === 1) result = result.filter(e => e.media_type === mediaTypes[0]);
    if (showFavoritesOnly) result = result.filter(e => e.is_favorite);
    events = result;
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
    renderMapInstance();
    const status = document.getElementById('filter-status');
    if (status) status.textContent = events.length + ' results';
  };

  if (collectionId) {
    fetch('/api/collections/' + collectionId + '/events')
      .then(r => r.json())
      .then((data: any[]) => {
        if (Array.isArray(data)) apply(data);
        else if (statusEl) statusEl.textContent = 'Filter error';
      })
      .catch(() => {
        const status = document.getElementById('filter-status');
        if (status) status.textContent = 'Filter error';
      });
    return;
  }

  apply(allEvents);
}

function clearAllFilters(): void {
  (document.getElementById('search-input') as HTMLInputElement).value = '';
  (document.getElementById('filter-person') as HTMLSelectElement).value = '';
  (document.getElementById('filter-location') as HTMLInputElement).value = '';
  (document.getElementById('filter-media-image') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-video') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-audio') as HTMLInputElement).checked = false;
  const collectionSel = document.getElementById('filter-collection') as HTMLSelectElement | null;
  if (collectionSel) collectionSel.value = '';
  selectedCollectionId = '';
  selectedFilterTags = [];
  renderSelectedFilterTags();
  events = allEvents;
  storyChunk = 1;
  renderStory();
  renderCalendar();
  updateStats();
  renderMapInstance();
  const status = document.getElementById('filter-status');
  if (status) status.textContent = '';
}

function addFilterTag(tag: string): void {
  if (!selectedFilterTags.includes(tag)) {
    selectedFilterTags.push(tag);
    renderSelectedFilterTags();
    applyAdvancedFilters();
  }
  (document.getElementById('filter-tag-input') as HTMLInputElement).value = '';
}

function removeFilterTag(tag: string): void {
  selectedFilterTags = selectedFilterTags.filter(t => t !== tag);
  renderSelectedFilterTags();
  applyAdvancedFilters();
}

function renderSelectedFilterTags(): void {
  const container = document.getElementById('selected-filter-tags');
  if (!container) return;
  container.innerHTML = selectedFilterTags.map(t =>
    '<span class="filter-tag-badge" onclick="removeFilterTag(\'' + escapeHtml(t) + '\')">' + escapeHtml(t) + ' <i class="fa-solid fa-xmark"></i></span>'
  ).join('');
}

function filterTagInput(): void {
  const input = (document.getElementById('filter-tag-input') as HTMLInputElement).value;
  if (input.endsWith(',') || input.endsWith(' ')) {
    const tag = input.replace(/[, ]+$/, '').trim();
    if (tag) addFilterTag(tag);
  }
}

function filterLocationDebounce(): void {
  clearTimeout(filterLocationTimeout);
  filterLocationTimeout = setTimeout(applyAdvancedFilters, 400);
}

async function loadStatsDist(): Promise<void> {
  const container = document.getElementById('stats-distribution-container');
  if (!container) return;
  container.innerHTML = '<div class="text-center text-muted py-5"><i class="fa-solid fa-spinner fa-spin me-2"></i>Loading statistics...</div>';

  try {
    const res = await fetch('/api/stats/distribution?year=' + currentYear);
    if (!res.ok) throw new Error('Failed');
    const dist = await res.json();

    const monthNames_short = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const maxMonth = Math.max(...(Object.values(dist.by_month) as number[]), 1);
    const monthBars = monthNames_short.map((m, i) => {
      const idx = String(i + 1).padStart(2, '0');
      const val = dist.by_month[idx] || 0;
      const pct = (val / maxMonth * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1">' +
        '<div class="stats-bar-value">' + val + '</div>' +
        '<div class="stats-bar" style="height:' + pct + '%" title="' + m + ': ' + val + ' events"></div>' +
        '<div class="stats-bar-label">' + m + '</div></div>';
    }).join('');

    const wdNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const maxWD = Math.max(...(Object.values(dist.by_weekday) as number[]), 1);
    const wdBars = wdNames.map((d, i) => {
      const val = dist.by_weekday[String(i)] || 0;
      const pct = (val / maxWD * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1">' +
        '<div class="weekday-bar" style="height:' + pct + '%" title="' + d + ': ' + val + ' events"></div>' +
        '<div class="weekday-bar-label">' + d + '</div></div>';
    }).join('');

    let tagHTML = '';
    if (dist.by_tag && dist.by_tag.length > 0) {
      const totalTags = dist.by_tag.reduce((s: number, t: any) => s + t.count, 0);
      tagHTML = '<div class="tag-cloud">' + dist.by_tag.slice(0, 20).map((t: any) => {
        const pct = totalTags > 0 ? (t.count / totalTags * 100).toFixed(1) : '0';
        return '<span class="tag-cloud-item" title="' + t.count + ' events">' + escapeHtml(t.name) + ' (' + pct + '%)</span>';
      }).join('') + '</div>';
    }

    let personHTML = '';
    if (dist.by_person && dist.by_person.length > 0) {
      personHTML = dist.by_person.map((p: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span>' + escapeHtml(p.name) + '</span><span class="badge bg-primary">' + p.count + '</span></div>'
      ).join('');
    }

    let userHTML = '';
    if (dist.by_user && dist.by_user.length > 0) {
      userHTML = dist.by_user.map((u: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span>' + escapeHtml(u.display_name) + '</span><span class="badge bg-primary">' + u.count + '</span></div>'
      ).join('');
    }

    let locHTML = '';
    if (dist.by_location && dist.by_location.length > 0) {
      locHTML = dist.by_location.map((l: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span><i class="fa-solid fa-location-dot me-1"></i>' + escapeHtml(l.location) + '</span><span class="badge bg-secondary">' + l.count + '</span></div>'
      ).join('');
    }

    const topDayFormatted = dist.top_day ? new Date(dist.top_day + 'T12:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric' }) : 'N/A';

    container.innerHTML =
      '<div class="row g-3">' +
      '<div class="col-12 mb-2"><div class="d-flex gap-3 flex-wrap">' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-calendar me-1"></i>Events</h5><div class="fs-4 fw-bold">' + dist.event_count + '</div><div class="text-muted small">' + dist.monthly_avg.toFixed(1) + '/mo · ' + dist.daily_avg.toFixed(2) + '/day</div></div>' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-star me-1"></i>Busiest Day</h5><div class="top-day-badge">' + topDayFormatted + '</div></div>' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-globe me-1"></i>Geo Spread</h5><div class="fs-5 fw-bold">' + (dist.geo_spread > 0 ? dist.geo_spread.toFixed(0) + ' km' : 'N/A') + '</div><div class="geo-spread">avg distance between locations</div></div>' +
      '</div></div>' +
      '<div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-chart-column me-1"></i>Events by Month</h5><div class="stats-bar-chart">' + monthBars + '</div></div></div>' +
      '<div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-calendar-week me-1"></i>Events by Day of Week</h5><div class="weekday-chart">' + wdBars + '</div></div></div>' +
      (tagHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-tags me-1"></i>Tag Distribution</h5>' + tagHTML + '</div></div>' : '') +
      (personHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-user-group me-1"></i>People</h5>' + personHTML + '</div></div>' : '') +
      (userHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-users me-1"></i>Family Members</h5>' + userHTML + '</div></div>' : '') +
      (locHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-map-pin me-1"></i>Top Locations</h5>' + locHTML + '</div></div>' : '') +
      '</div>';
  } catch (err) {
    container.innerHTML = '<div class="text-center text-muted py-5">Failed to load statistics</div>';
  }
}

async function loadEvents(): Promise<void> {
  try {
    // The story is one continuous multi-year roll, so load everything at once.
    // Authenticated users get every event; guests fall back to public events.
    const res = await fetch('/api/events/full');
    if (res.status === 401) {
      const pubRes = await fetch('/api/public');
      events = pubRes.ok ? await pubRes.json() as TimelineEvent[] : [];
    } else if (res.ok) {
      events = await res.json() as TimelineEvent[];
    } else {
      events = [];
    }
    if (!Array.isArray(events)) events = [];
    allEvents = events;
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
  } catch (err) {
    console.error('Failed to load events:', err);
  }
}

async function loadUsers(): Promise<void> {
  try {
    const res = await fetch('/api/users');
    users = await res.json();
    if (!Array.isArray(users)) users = [];
  } catch (e) { users = []; }
}

async function toggleFav(id: number): Promise<void> {
  const csrf = await ensureCSRF();
  await fetch('/api/events/favorite', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    body: JSON.stringify({ id })
  });
  await loadData();
  loadWrapped();
}

async function loadCollections(): Promise<void> {
  try {
    const res = await fetch('/api/collections');
    collections = await res.json();
    const sel = document.getElementById('filter-collection') as HTMLSelectElement;
    if (sel) {
      sel.innerHTML = '<option value="">All Collections</option>' + collections.map((c: any) => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');
    }
  } catch (e) { collections = []; }
}

function toggleFavFilter(): void {
  showFavoritesOnly = !showFavoritesOnly;
  const btn = document.getElementById('fav-filter-btn');
  if (btn) btn.classList.toggle('btn-primary', showFavoritesOnly);
  if (btn) btn.classList.toggle('btn-outline-primary', !showFavoritesOnly);
  applyAdvancedFilters();
}

async function filterByCollection(): Promise<void> {
  const sel = document.getElementById('filter-collection') as HTMLSelectElement;
  selectedCollectionId = sel?.value || '';
  if (!selectedCollectionId) {
    events = allEvents;
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
    return;
  }
  try {
    const res = await fetch('/api/collections/' + selectedCollectionId + '/events');
    events = await res.json();
    if (!Array.isArray(events)) events = [];
    if (showFavoritesOnly) {
      events = events.filter(e => e.is_favorite);
    }
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
  } catch (e) { console.error('Filter by collection failed', e); }
}

async function ensureCSRF(): Promise<string> {
  try {
    const res = await fetch('/api/csrf-token');
    if (res.ok) {
      const data = await res.json();
      return data.token;
    }
  } catch (e) {}
  return '';
}

async function loadData(): Promise<void> {
  await Promise.all([loadEvents(), loadContributions(), loadUsers(), loadCollections()]);
  populateYearButtons();
  storyChunk = 1;
}

function populateYearButtons(): void {
  const container = document.getElementById('year-buttons');
  if (!container) return;
  const years = new Set<number>();
  events.forEach(e => {
    const y = parseInt(e.date.slice(0, 4));
    if (!isNaN(y)) years.add(y);
  });
  years.add(currentYear);
  const sorted = Array.from(years).sort((a, b) => b - a);
  container.innerHTML = sorted.map(y =>
    `<button class="btn ${y === currentYear ? 'btn-primary' : 'btn-outline-primary'}" data-year="${y}" onclick="changeYear(${y})">${y}</button>`
  ).join('');
}

// ── The Story ──
// One continuous multi-year roll: photos, videos, audio, places (mini-maps),
// and text events (books, quotes, notes) all inline, newest memory first.

function filteredEvents(): TimelineEvent[] {
  return currentMonth > 0
    ? events.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
    : events;
}

function renderStory(): void {
  const container = document.getElementById('timeline-container');
  if (!container) return;
  const list = filteredEvents();

  const emptyState = `
    <div class="story-empty text-center text-muted py-5">
      <i class="fa-regular fa-hourglass-half fa-3x mb-3"></i>
      <p class="fw-bold mb-1">Your story starts here</p>
      <p class="small">Add photos, videos, places, books and quotes in the Admin panel — they will appear here, year after year, in one continuous timeline.</p>
    </div>
  `;

  if (list.length === 0) {
    container.innerHTML = emptyState;
    return;
  }

  // Year marker counts (stable across chunks)
  const yearCounts: Record<number, number> = {};
  list.forEach(e => {
    const y = parseInt(e.date.slice(0, 4));
    if (!isNaN(y)) yearCounts[y] = (yearCounts[y] || 0) + 1;
  });

  const start = 0;
  const end = Math.min(storyChunk * storyChunkSize, list.length);
  let html = '';
  let prevYear = -1;
  let prevMonth = -1;

  list.slice(start, end).forEach(e => {
    const eventDate = new Date(e.date);
    const year = parseInt(e.date.slice(0, 4));
    const month = eventDate.getMonth();

    if (year !== prevYear) {
      prevYear = year;
      prevMonth = -1;
      const count = yearCounts[year] || 0;
      html += `
        <div class="story-year" id="year-${year}" data-year="${year}">
          <div class="story-year-rule"></div>
          <h2 class="story-year-label">${year}</h2>
          <div class="story-year-rule"></div>
          <span class="story-year-count">${count} ${count === 1 ? 'moment' : 'moments'}</span>
        </div>
      `;
    }
    if (month !== prevMonth) {
      prevMonth = month;
      html += `
        <div class="story-month">
          <span class="badge bg-primary">${monthNames[month]}</span>
        </div>
      `;
    }

    html += storyCardHtml(e);
  });

  container.innerHTML = html;
  initStoryObservers();
  initMiniMaps();
}

function storyCardHtml(e: TimelineEvent): string {
  const hasMedia = !!e.media_url;
  const hasGeo = !!(e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));
  const isVideo = e.media_type === 'video';
  const isAudio = e.media_type === 'audio';
  const tagList = e.tags ? e.tags.split(',').map(t => t.trim()).filter(t => t) : [];

  let weatherHtml = '';
  if (e.weather_data) {
    try {
      const w = JSON.parse(e.weather_data) as Weather;
      weatherHtml = `<span class="weather-badge ms-2"><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</span>`;
    } catch (_) {}
  }

  let userHtml = '';
  if (e.user_id && users.length) {
    const u = users.find(u => u.id === e.user_id);
    if (u) {
      userHtml = `<span class="user-badge ms-1" style="background:${u.color || '#7c3aed'}"><i class="fa-solid fa-user"></i> ${escapeHtml(u.display_name || u.username)}</span>`;
    }
  }

  const recurringBadge = e.recurring ? `<span class="badge bg-info ms-1"><i class="fa-solid fa-rotate"></i> ${e.recurring}</span>` : '';
  const favStar = `<i class="${e.is_favorite ? 'fa-solid' : 'fa-regular'} fa-star text-warning story-fav" onclick="event.stopPropagation();toggleFav(${e.id})" title="${e.is_favorite ? 'Unfavorite' : 'Favorite'}"></i>`;

  let mediaHtml = '';
  if (hasMedia) {
    if (isVideo) {
      mediaHtml = `
        <div class="story-media-frame">
          <video class="story-media-el" src="${escapeHtml(e.media_url)}" muted preload="metadata"></video>
          <span class="story-play"><i class="fa-solid fa-play"></i></span>
        </div>
      `;
    } else if (isAudio) {
      mediaHtml = `
        <div class="story-audio">
          <i class="fa-solid fa-music"></i>
          <span>Audio memory — tap to play</span>
        </div>
      `;
    } else {
      mediaHtml = `
        <div class="story-media-frame">
          <img class="story-media-el" src="${escapeHtml(e.thumbnail || e.media_url)}" alt="${escapeHtml(e.title)}" loading="lazy">
        </div>
      `;
    }
  }

  let mapHtml = '';
  if (hasGeo) {
    mapHtml = `
      <div class="story-minimap" data-lat="${e.latitude}" data-lng="${e.longitude}" data-id="${e.id}">
        <span class="story-minimap-hint"><i class="fa-solid fa-map-location-dot"></i> Open map</span>
      </div>
    `;
  }

  const cardClass = hasMedia ? 'story-media' : 'story-text';
  const geoClass = hasGeo ? ' story-geo' : '';

  return `
    <article class="story-card ${cardClass}${geoClass}" id="event-${e.id}" onclick="${hasMedia ? 'showMedia(' + e.id + ')' : ''}">
      ${mediaHtml}
      <div class="story-body">
        <div class="d-flex justify-content-between align-items-start gap-2">
          <div class="story-meta">
            <i class="fa-solid fa-calendar-day me-1"></i>${formatDate(e.date)}
            ${e.start_time ? '<span class="story-meta-time"><i class="fa-regular fa-clock ms-2 me-1"></i>' + e.start_time.substring(0, 5) + '</span>' : ''}
            ${e.end_time ? '–' + e.end_time.substring(0, 5) : ''}
            ${recurringBadge}
          </div>
          ${favStar}
        </div>
        <div class="story-title">${escapeHtml(e.title)} ${weatherHtml}</div>
        ${e.location ? '<div class="story-location"><i class="fa-solid fa-location-dot me-1"></i>' + escapeHtml(e.location) + userHtml + '</div>' : userHtml ? '<div class="story-location">' + userHtml + '</div>' : ''}
        ${tagList.length > 0 ? '<div class="story-tags"><i class="fa-solid fa-tags me-1"></i>' + tagList.map(t => '<span class="badge bg-secondary me-1">' + escapeHtml(t) + '</span>').join('') + '</div>' : ''}
        ${e.description ? '<div class="story-desc md-content">' + renderMarkdown(e.description) + '</div>' : ''}
        ${mapHtml}
      </div>
    </article>
  `;
}

let miniMaps: Map<number, any> = new Map();

function initMiniMaps(): void {
  // Destroy maps whose containers were re-rendered, then (re)create one per geo card.
  miniMaps.forEach(m => { try { m.remove(); } catch (_) {} });
  miniMaps.clear();

  document.querySelectorAll<HTMLElement>('.story-minimap[data-lat]').forEach(el => {
    const lat = parseFloat(el.dataset.lat || '0');
    const lng = parseFloat(el.dataset.lng || '0');
    const id = parseInt(el.dataset.id || '0');
    if (!lat && !lng) return;
    const map = L.map(el, {
      zoomControl: false,
      attributionControl: false,
      scrollWheelZoom: false,
      dragging: false,
      touchZoom: false,
      doubleClickZoom: false,
      boxZoom: false,
      keyboard: false
    }).setView([lat, lng], 10);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 18 }).addTo(map);
    L.marker([lat, lng], {
      icon: L.divIcon({
        className: 'custom-marker',
        html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:20px;"></i>',
        iconSize: [20, 20],
        iconAnchor: [10, 20],
        popupAnchor: [0, -20]
      })
    }).addTo(map);
    miniMaps.set(id, map);
  });
}

function initStoryObservers(): void {
  const container = document.getElementById('timeline-container');

  // Keep the "Viewing YYYY" indicator in the toolbar in sync with the scroll position.
  if (storyYearObserver) storyYearObserver.disconnect();
  const yearEls = container ? container.querySelectorAll('.story-year[data-year]') : [];
  if (yearEls.length > 0) {
    storyYearObserver = new IntersectionObserver((entries) => {
      entries.forEach(en => {
        if (en.isIntersecting) {
          const y = parseInt((en.target as HTMLElement).dataset.year || '0');
          if (y) setViewedYear(y);
        }
      });
    }, { rootMargin: '-35% 0px -55% 0px' });
    yearEls.forEach(el => storyYearObserver!.observe(el));
  }

  // Infinite scroll: when the sentinel becomes visible, render the next chunk.
  const sentinel = document.getElementById('story-sentinel');
  if (!sentinel) return;
  if (storySentinelObserver) storySentinelObserver.disconnect();
  const total = filteredEvents().length;
  const shown = Math.min(storyChunk * storyChunkSize, total);
  if (total === 0 || shown >= total) {
    sentinel.style.display = 'none';
    return;
  }
  sentinel.style.display = '';
  storySentinelObserver = new IntersectionObserver((entries) => {
    if (entries.some(en => en.isIntersecting)) loadMoreGallery();
  }, { rootMargin: '300px' });
  storySentinelObserver.observe(sentinel);
}

function setViewedYear(year: number): void {
  if (year === currentYear) return;
  currentYear = year;
  const yearEl = document.getElementById('current-year');
  if (yearEl) yearEl.textContent = String(year);
  document.querySelectorAll('.year-selector .btn[data-year]').forEach(b => {
    const active = b.getAttribute('data-year') === String(year);
    b.classList.toggle('btn-primary', active);
    b.classList.toggle('btn-outline-primary', !active);
  });
  const icsLink = document.getElementById('ics-download') as HTMLAnchorElement;
  if (icsLink) icsLink.href = '/api/events/ics?year=' + year;
}

async function loadMoreGallery(): Promise<void> {
  const total = filteredEvents().length;
  if (storyChunk * storyChunkSize >= total) return;
  storyChunk++;
  renderStory();
}

function renderMarkdown(text: string): string {
  if (!text) return '';
  let html = escapeHtml(text);
  html = html.replace(/### (.+)/g, '<h3>$1</h3>');
  html = html.replace(/## (.+)/g, '<h2>$1</h2>');
  html = html.replace(/# (.+)/g, '<h1>$1</h1>');
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
  html = html.replace(/`(.+?)`/g, '<code>$1</code>');
  html = html.replace(/^> (.+)$/gm, '<blockquote>$1</blockquote>');
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>');
  html = html.replace(/(<li>.*<\/li>\n?)/s, '<ul>$1</ul>');
  html = html.replace(/\[(.+?)\]\((.+?)\)/g, '<a href="$2" target="_blank">$1</a>');
  html = html.replace(/\n/g, '<br>');
  return html;
}

function getMediaIcon(mediaType: string): string {
  switch (mediaType) {
    case 'video': return 'fa-solid fa-video';
    case 'audio': return 'fa-solid fa-music';
    default: return 'fa-solid fa-image';
  }
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function ensureMapInstance(): any {
  if (mapInstance) return mapInstance;
  const el = document.getElementById('map-container');
  if (!el) return null;
  mapInstance = L.map('map-container').setView([20, 0], 2);
  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
    maxZoom: 18
  }).addTo(mapInstance);
  setTimeout(() => mapInstance.invalidateSize(), 100);
  return mapInstance;
}

function renderMapInstance(): void {
  const eventList = Array.isArray(events) ? events : [];
  const filtered = currentMonth > 0
    ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
    : eventList;
  const geoEvents = filtered.filter(e => e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));

  const placeholder = document.getElementById('map-placeholder');
  if (placeholder) placeholder.style.display = geoEvents.length ? 'none' : 'block';

  const map = ensureMapInstance();
  if (!map) return;

  if (!geoEvents.length) {
    mapMarkers.forEach(m => map.removeLayer(m));
    mapMarkers = [];
    if (mapPathLine) { map.removeLayer(mapPathLine); mapPathLine = null; }
    return;
  }

  mapMarkers.forEach(m => map.removeLayer(m));
  mapMarkers = [];

  const bounds: [number, number][] = [];
  const markerIcon = L.divIcon({
    className: 'custom-marker',
    html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:24px;"></i>',
    iconSize: [24, 24],
    iconAnchor: [12, 24],
    popupAnchor: [0, -24]
  });

  geoEvents.forEach(e => {
    const m = L.marker([e.latitude!, e.longitude!], { icon: markerIcon }).addTo(mapInstance);
    let weatherHtml = '';
    if (e.weather_data) {
      try {
        const w = JSON.parse(e.weather_data);
        weatherHtml = `<br><small><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</small>`;
      } catch (_) { }
    }
    m.bindPopup(`
      <div class="map-popup">
        <h6>${escapeHtml(e.title)}</h6>
        <p>${formatDate(e.date)} — ${escapeHtml(e.location)}${weatherHtml}</p>
      </div>
    `);
    mapMarkers.push(m);
    bounds.push([e.latitude!, e.longitude!]);
  });

  const showPath = (document.getElementById('show-location-path') as HTMLInputElement)?.checked;
  if (mapPathLine) {
    mapInstance.removeLayer(mapPathLine);
    mapPathLine = null;
  }
  if (showPath && geoEvents.length > 1) {
    const sorted = [...geoEvents].sort((a, b) => a.date.localeCompare(b.date));
    const latlngs: [number, number][] = sorted.map(e => [e.latitude!, e.longitude!]);
    mapPathLine = L.polyline(latlngs, {
      color: '#7c3aed',
      weight: 3,
      opacity: 0.6,
      dashArray: '8, 8'
    }).addTo(mapInstance);
  }

  if (bounds.length > 0) {
    mapInstance.fitBounds(bounds, { padding: [30, 30], maxZoom: 14 });
  }

  setTimeout(() => mapInstance.invalidateSize(), 100);
}

// ── Overlays (Calendar / Map / Stats) ──

function openOverlay(id: string): void {
  const overlay = document.getElementById(id);
  if (!overlay) return;
  overlay.style.display = 'flex';
  document.body.style.overflow = 'hidden';
  if (id === 'map-overlay') {
    renderMapInstance();
    setTimeout(() => { if (mapInstance) mapInstance.invalidateSize(); }, 150);
  } else if (id === 'stats-overlay') {
    const yearEl = document.getElementById('stats-year');
    if (yearEl) yearEl.textContent = String(currentYear);
    const cyEl = document.getElementById('contribution-year');
    if (cyEl) cyEl.textContent = '· ' + currentYear;
    loadStatsDist();
    loadContributions();
  } else if (id === 'calendar-overlay') {
    renderCalendarView();
  }
}

function closeOverlay(id: string): void {
  const overlay = document.getElementById(id);
  if (!overlay) return;
  overlay.style.display = 'none';
  // Keep body scroll lock while any overlay is still open.
  const anyOpen = document.querySelectorAll('.overlay[style*="flex"]').length > 0;
  if (!anyOpen) document.body.style.overflow = '';
}

function openMapAt(lat: number, lng: number, eventId?: number): void {
  openOverlay('map-overlay');
  const map = ensureMapInstance();
  if (!map) return;
  map.setView([lat, lng], 12);
  // Highlight the matching card in the story so the user can jump back.
  if (eventId) {
    const card = document.getElementById('event-' + eventId);
    if (card) {
      card.scrollIntoView({ behavior: 'smooth', block: 'center' });
      card.classList.add('story-card-flash');
      setTimeout(() => card.classList.remove('story-card-flash'), 1600);
    }
  }
}

// ── Lightbox State ──
let lightboxEvents: TimelineEvent[] = [];
let lightboxIndex = -1;
let lightboxZoomed = false;
let touchStartX = 0;
let touchStartY = 0;
let lightboxOpen = false;
let lightboxKeyHandler: ((e: KeyboardEvent) => void) | null = null;
let lightboxTouchHandler: ((e: TouchEvent) => void) | null = null;

function showMedia(id: number): void {
  lightboxEvents = Array.isArray(events) ? events.filter(e => e.media_url) : [];
  lightboxIndex = lightboxEvents.findIndex(e => e.id === id);
  if (lightboxIndex === -1) return;
  lightboxZoomed = false;
  renderLightbox();
  openLightbox();
}

function openLightbox(): void {
  const lb = document.getElementById('lightbox');
  if (!lb) return;
  lightboxOpen = true;
  lb.style.display = 'flex';
  document.body.style.overflow = 'hidden';

  lightboxKeyHandler = (e: KeyboardEvent) => {
    if (e.key === 'Escape') { closeLightbox(); return; }
    if (e.key === 'ArrowLeft') { e.preventDefault(); navigateLightbox(-1); }
    if (e.key === 'ArrowRight') { e.preventDefault(); navigateLightbox(1); }
  };
  document.addEventListener('keydown', lightboxKeyHandler);

  lightboxTouchHandler = (e: TouchEvent) => {
    if (!lightboxOpen) return;
    if (e.type === 'touchstart') {
      touchStartX = e.touches[0].clientX;
      touchStartY = e.touches[0].clientY;
    } else if (e.type === 'touchend') {
      const dx = e.changedTouches[0].clientX - touchStartX;
      const dy = e.changedTouches[0].clientY - touchStartY;
      if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
        navigateLightbox(dx > 0 ? -1 : 1);
      }
    }
  };
  lb.addEventListener('touchstart', lightboxTouchHandler);
  lb.addEventListener('touchend', lightboxTouchHandler);
}

function closeLightbox(): void {
  const lb = document.getElementById('lightbox');
  if (!lb) return;
  lightboxOpen = false;
  lb.style.display = 'none';
  document.body.style.overflow = '';
  lightboxZoomed = false;
  if (lightboxKeyHandler) document.removeEventListener('keydown', lightboxKeyHandler);
  if (lightboxTouchHandler) {
    lb.removeEventListener('touchstart', lightboxTouchHandler);
    lb.removeEventListener('touchend', lightboxTouchHandler);
  }
}

function navigateLightbox(dir: number): void {
  if (lightboxEvents.length === 0) return;
  lightboxIndex = (lightboxIndex + dir + lightboxEvents.length) % lightboxEvents.length;
  lightboxZoomed = false;
  renderLightbox();
}

function toggleLightboxZoom(img: HTMLImageElement): void {
  if (img.dataset.zoomed === 'true') {
    img.dataset.zoomed = 'false';
    const container = document.getElementById('lightbox-media-container');
    if (container) container.classList.remove('zoomed');
  } else {
    img.dataset.zoomed = 'true';
    const container = document.getElementById('lightbox-media-container');
    if (container) container.classList.add('zoomed');
  }
}

function renderLightbox(): void {
  const event = lightboxEvents[lightboxIndex];
  if (!event) return;

  const container = document.getElementById('lightbox-media-container');
  const titleEl = document.getElementById('lightbox-title');
  const descEl = document.getElementById('lightbox-desc');
  const counterEl = document.getElementById('lightbox-counter');
  const loaderEl = document.getElementById('lightbox-loader');

  if (titleEl) titleEl.textContent = event.title;
  if (counterEl) counterEl.textContent = `${lightboxIndex + 1} / ${lightboxEvents.length}`;

  if (descEl) {
    const dateParts: string[] = [];
    if (event.start_time) {
      dateParts.push(`<i class="fa-regular fa-clock me-1"></i>${event.start_time.substring(0, 5)}${event.end_time ? '–' + event.end_time.substring(0, 5) : ''}`);
    }
    if (event.location) {
      dateParts.push(`<i class="fa-solid fa-location-dot ms-2 me-1"></i>${escapeHtml(event.location)}`);
    }
    const dateInfo = event.date + (dateParts.length ? ' ' + dateParts.join('') : '');
    descEl.innerHTML = `
      <div class="lightbox-date"><i class="fa-solid fa-calendar me-1"></i>${dateInfo}</div>
      ${event.description ? '<div class="lightbox-description">' + renderMarkdown(event.description) + '</div>' : ''}
    `;
  }

  if (loaderEl) loaderEl.style.display = 'flex';

  if (!container) return;
  container.innerHTML = '';
  container.classList.remove('zoomed');

  if (event.media_type === 'video') {
    const video = document.createElement('video');
    video.className = 'lightbox-media lightbox-video';
    video.src = event.media_url;
    video.controls = true;
    video.autoplay = true;
    container.appendChild(video);
    if (loaderEl) loaderEl.style.display = 'none';
  } else if (event.media_type === 'audio') {
    const audio = document.createElement('audio');
    audio.className = 'lightbox-media lightbox-audio';
    audio.src = event.media_url;
    audio.controls = true;
    audio.autoplay = true;
    container.appendChild(audio);
    if (loaderEl) loaderEl.style.display = 'none';
  } else {
    const img = new Image();
    img.onload = () => {
      if (loaderEl) loaderEl.style.display = 'none';
      container!.innerHTML = '';
      img.className = 'lightbox-image';
      img.alt = event.title || 'Event media';
      img.draggable = false;
      img.onclick = () => toggleLightboxZoom(img);
      container!.appendChild(img);
    };
    img.onerror = () => {
      if (loaderEl) loaderEl.style.display = 'none';
      container!.innerHTML = '<p class="text-white-50 mt-5">Failed to load image</p>';
    };
    img.src = event.media_url;
  }
}

function escapeHtml(text: string): string {
  if (!text) return '';
  return text.replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

let calendarYear: number = new Date().getFullYear();
let calendarMonth: number = new Date().getMonth() + 1;
let calendarEventList: TimelineEvent[] = [];

function renderCalendar(): void {
  const filtered = Array.isArray(events) ? events : [];
  calendarEventList = filtered;
}

function renderCalendarView(): void {
  const grid = document.getElementById('calendar-grid');
  const title = document.getElementById('calendar-title');
  if (!grid || !title) return;

  title.textContent = monthNames[calendarMonth - 1] + ' ' + calendarYear;

  const firstDay = new Date(calendarYear, calendarMonth - 1, 1);
  const lastDay = new Date(calendarYear, calendarMonth, 0);
  const startDay = firstDay.getDay();
  const daysInMonth = lastDay.getDate();

  const prevMonth = new Date(calendarYear, calendarMonth - 1, 0);
  const daysInPrevMonth = prevMonth.getDate();

  let html = '';
  const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  dayNames.forEach(d => {
    html += '<div class="calendar-header">' + d + '</div>';
  });

  for (let i = startDay - 1; i >= 0; i--) {
    const day = daysInPrevMonth - i;
    html += '<div class="calendar-day other-month"><span class="day-num">' + day + '</span></div>';
  }

  const today = new Date();
  const todayStr = today.toISOString().split('T')[0];

  for (let day = 1; day <= daysInMonth; day++) {
    const dateStr = calendarYear + '-' + String(calendarMonth).padStart(2, '0') + '-' + String(day).padStart(2, '0');
    const dayEvents = calendarEventList.filter(e => e.date === dateStr);
    const isToday = dateStr === todayStr;

    html += '<div class="calendar-day' + (isToday ? ' today' : '') + '" onclick="showCalendarDay(\'' + dateStr + '\')">';
    html += '<span class="day-num">' + day + '</span>';
    if (dayEvents.length > 0) {
      html += '<div class="event-dots">';
      dayEvents.slice(0, 5).forEach(() => {
        html += '<span class="event-dot"></span>';
      });
      if (dayEvents.length > 5) {
        html += '<span class="event-dot" style="background:var(--text-muted)"></span>';
      }
      html += '</div>';
    }
    html += '</div>';
  }

  const remainingCells = 7 - ((startDay + daysInMonth) % 7);
  if (remainingCells < 7) {
    for (let day = 1; day <= remainingCells; day++) {
      html += '<div class="calendar-day other-month"><span class="day-num">' + day + '</span></div>';
    }
  }

  grid.innerHTML = html;
}

function calendarPrevMonth(): void {
  calendarMonth--;
  if (calendarMonth < 1) { calendarMonth = 12; calendarYear--; }
  renderCalendarView();
}

function calendarNextMonth(): void {
  calendarMonth++;
  if (calendarMonth > 12) { calendarMonth = 1; calendarYear++; }
  renderCalendarView();
}

function calendarToday(): void {
  calendarYear = new Date().getFullYear();
  calendarMonth = new Date().getMonth() + 1;
  renderCalendarView();
}

function showCalendarDay(dateStr: string): void {
  const dayEvents = calendarEventList.filter(e => e.date === dateStr);
  const section = document.getElementById('calendar-selected-day');
  const dateEl = document.getElementById('calendar-selected-date');
  const listEl = document.getElementById('calendar-event-list');
  if (!section || !dateEl || !listEl) return;

  const d = new Date(dateStr + 'T12:00:00');
  dateEl.textContent = d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });

  if (dayEvents.length === 0) {
    listEl.innerHTML = '<p class="text-muted text-center py-3">No events on this day</p>';
  } else {
    listEl.innerHTML = dayEvents.map(e => {
      const mediaIcon = getMediaIcon(e.media_type);
      return '<div class="calendar-event-item" onclick="showMedia(' + e.id + ')">'
        + '<div class="fw-bold">' + escapeHtml(e.title) + '</div>'
        + '<div class="text-muted small">' + escapeHtml(e.location)
        + (e.media_url ? ' <i class="' + mediaIcon + ' ms-1"></i>' : '')
        + '</div>'
        + '</div>';
    }).join('');
  }
  section.style.display = 'block';
}

async function loadMemories(): Promise<void> {
  try {
    const res = await fetch('/api/memories');
    const memories = await res.json();
    const section = document.getElementById('memories-section');
    if (!section) return;
    if (!memories || !memories.length) { section.style.display = 'none'; return; }
    memories.sort((a: any, b: any) => a.years_ago - b.years_ago);
    section.style.display = 'block';
    section.innerHTML = '<h5 class="mb-3"><i class="fa-solid fa-clock-rotate-left me-2 text-primary"></i>On This Day</h5>'
      + memories.map((m: any) => '<div class="memories-item"><div class="fw-bold">' + escapeHtml(m.title) + '</div><div class="small text-muted">' + m.years_ago + ' year' + (m.years_ago > 1 ? 's' : '') + ' ago &middot; ' + m.date + '</div></div>').join('');
  } catch (_) { }
}

async function loadWrapped(): Promise<void> {
  const container = document.getElementById('wrapped-content');
  if (!container) return;
  try {
    const res = await fetch('/api/wrapped?year=' + currentYear);
    if (!res.ok) { container.innerHTML = '<div class="text-center text-muted py-5">Failed to load wrapped data</div>'; return; }
    const w = await res.json();

    const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const maxMonth = Math.max(...(Object.values(w.by_month) as number[]), 1);
    const monthBars = monthNames.map((m, i) => {
      const idx = String(i + 1).padStart(2, '0');
      const val = w.by_month[idx] || 0;
      const pct = (val / maxMonth * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1"><div class="stats-bar-value">' + val + '</div><div class="stats-bar" style="height:' + pct + '%;background:var(--primary);border-radius:4px 4px 0 0" title="' + m + ': ' + val + '"></div><div class="stats-bar-label">' + m + '</div></div>';
    }).join('');

    container.innerHTML = `
      <div class="text-center mb-4">
        <h3 class="fw-bold"><i class="fa-solid fa-gift me-2 text-primary"></i>${w.year} Wrapped</h3>
        <p class="text-muted">Your year in review</p>
      </div>
      <div class="row g-3 mb-4">
        <div class="col-md-3"><div class="dist-card text-center"><h5>Total Events</h5><div class="fs-2 fw-bold text-primary">${w.total_events}</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Longest Streak</h5><div class="fs-2 fw-bold text-primary">${w.longest_streak} days</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Favorites</h5><div class="fs-2 fw-bold text-warning">${w.favorite_count}</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Media Items</h5><div class="fs-2 fw-bold text-primary">${w.total_media}</div></div></div>
      </div>
      <div class="row g-3 mb-4">
        <div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-star me-2 text-warning"></i>Busiest Month</h5><div class="fs-4 fw-bold">${w.busiest_month || 'N/A'}</div><div class="text-muted">${w.busiest_month_count || 0} events</div></div></div>
        <div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-pen me-2"></i>Most Detailed Event</h5><div class="fw-bold">${w.top_event ? escapeHtml(w.top_event) : 'N/A'}</div>${w.top_event_date ? '<div class="text-muted small">' + w.top_event_date + '</div>' : ''}</div></div>
      </div>
      ${w.most_tags_title ? '<div class="row g-3 mb-4"><div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-tags me-2"></i>Most Tagged Event</h5><div class="fw-bold">' + escapeHtml(w.most_tags_title) + '</div><div class="text-muted">' + w.most_tags_count + ' tags</div></div></div></div>' : ''}
      <div class="dist-card">
        <h5><i class="fa-solid fa-chart-column me-2"></i>Events by Month</h5>
        <div class="stats-bar-chart" style="display:flex;align-items:flex-end;gap:6px;height:150px;padding-top:8px">${monthBars}</div>
      </div>
    `;
  } catch (e) {
    container.innerHTML = '<div class="text-center text-muted py-5">Failed to load wrapped data</div>';
  }
}

function loadAnalytics(): void {
  fetch('/api/config').then(function (r) { return r.json(); }).then(function (cfg) {
    if (cfg.umami_url && cfg.umami_site && cfg.umami_enabled) {
      var s = document.createElement('script');
      s.async = true;
      s.defer = true;
      s.src = cfg.umami_url + '/script.js';
      s.setAttribute('data-website-id', cfg.umami_site);
      document.head.appendChild(s);
    }
  }).catch(function () { });
}

function initApp(): void {
  initTheme();
  loadAnalytics();

  const params = new URLSearchParams(window.location.search);
  const q = params.get('q');
  if (q) {
    const input = document.getElementById('search-input') as HTMLInputElement | null;
    if (input) input.value = q;
  }

  loadData();
  loadMemories();
  renderCalendarView();
  loadPersonsForFilter();

  // Escape closes any open overlay.
  document.addEventListener('keydown', (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      document.querySelectorAll<HTMLElement>('.overlay').forEach(ov => {
        if (ov.style.display === 'flex') closeOverlay(ov.id);
      });
    }
  });

  fetch('/api/version').then(r => r.json()).then(d => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v' + d.version;
  }).catch(() => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v1.0.0';
  });
}

async function loadPersonsForFilter(): Promise<void> {
  try {
    const res = await fetch('/api/persons');
    const persons = await res.json();
    if (!Array.isArray(persons)) return;
    const select = document.getElementById('filter-person') as HTMLSelectElement;
    if (!select) return;
    select.innerHTML = '<option value="">Any person</option>';
    persons.forEach((p: any) => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = p.name;
      select.appendChild(opt);
    });
  } catch (_) { }
}

document.addEventListener('DOMContentLoaded', initApp);

(window as any).changeYear = changeYear;
(window as any).searchEvents = searchEvents;
(window as any).filterMonth = filterMonth;
(window as any).showMedia = showMedia;
(window as any).loadMoreGallery = loadMoreGallery;
(window as any).calendarPrevMonth = calendarPrevMonth;
(window as any).calendarNextMonth = calendarNextMonth;
(window as any).calendarToday = calendarToday;
(window as any).showCalendarDay = showCalendarDay;
(window as any).toggleFilters = toggleFilters;
(window as any).applyAdvancedFilters = applyAdvancedFilters;
(window as any).clearAllFilters = clearAllFilters;
(window as any).addFilterTag = addFilterTag;
(window as any).removeFilterTag = removeFilterTag;
(window as any).filterTagInput = filterTagInput;
(window as any).filterLocationDebounce = filterLocationDebounce;
(window as any).loadStatsDist = loadStatsDist;
(window as any).globalSearchInput = globalSearchInput;
(window as any).globalSearchKeydown = globalSearchKeydown;
(window as any).globalSearchFocus = globalSearchFocus;
(window as any).selectGlobalResult = selectGlobalResult;
(window as any).highlightGlobalItem = highlightGlobalItem;
(window as any).toggleFav = toggleFav;
(window as any).toggleFavFilter = toggleFavFilter;
(window as any).filterByCollection = filterByCollection;
(window as any).loadWrapped = loadWrapped;
(window as any).openOverlay = openOverlay;
(window as any).closeOverlay = closeOverlay;
(window as any).openMapAt = openMapAt;
