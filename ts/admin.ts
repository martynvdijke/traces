export {};
declare const bootstrap: any;
declare const L: any;

let events: any[] = [];
let persons: any[] = [];
let allPersons: any[] = [];
let filteredEvents: any[] = [];
const eventModal = new bootstrap.Modal(document.getElementById('eventModal'));
const personModal = new bootstrap.Modal(document.getElementById('personModal'));
let locationMap: any = null;
let locationMarker: any = null;
let searchTimeout: any = null;
let viewingPersonId: number | null = null;
let eventPhotoUrl = '';

async function init(): Promise<void> {
  await ensureCSRF();
  populateYearFilter();
  await loadEvents();
  await loadPersons();
  loadGotifyConfig();
  loadMemoriesConfig();
  loadEmailConfig();
  loadMemories();
  loadUsers();
  loadOllamaConfig();
  loadBackups();
}

function loadAdminAnalytics(): void {
  fetch('/api/config').then(function (r) { return r.json(); }).then(function (cfg) {
    if (cfg.umami_url && cfg.umami_site) {
      var s = document.createElement('script');
      s.async = true;
      s.defer = true;
      s.src = cfg.umami_url + '/script.js';
      s.setAttribute('data-website-id', cfg.umami_site);
      document.head.appendChild(s);
    }
  }).catch(function () { });
}

function populateYearFilter(): void {
  const select = document.getElementById('filter-year') as HTMLSelectElement;
  if (!select) return;
  const y = new Date().getFullYear();
  for (let i = y + 1; i >= y - 10; i--) {
    const opt = document.createElement('option');
    opt.value = String(i);
    opt.textContent = String(i);
    if (i === y) opt.selected = true;
    select.appendChild(opt);
  }
}

function debouncedSearch(): void {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(applyFilters, 300);
}

function applyFilters(): void {
  const q = (document.getElementById('event-search') as HTMLInputElement).value.toLowerCase();
  const personId = (document.getElementById('filter-person') as HTMLSelectElement).value;
  const mediaType = (document.getElementById('filter-media') as HTMLSelectElement).value;
  const year = (document.getElementById('filter-year') as HTMLSelectElement).value;

  filteredEvents = events.filter((e: any) => {
    if (q && !e.title.toLowerCase().includes(q) && !e.description.toLowerCase().includes(q) && !e.location.toLowerCase().includes(q) && !getPersonName(e.person_id).toLowerCase().includes(q)) return false;
    if (personId && String(e.person_id) !== personId) return false;
    if (mediaType && e.media_type !== mediaType) return false;
    if (year && e.date.slice(0, 4) !== year) return false;
    return true;
  });
  renderEventList();
}

async function loadEvents(): Promise<void> {
  try {
    const res = await fetch('/api/events/full');
    events = await res.json();
    filteredEvents = [...events];
    renderEventList();
  } catch (e) { console.error('Failed to load events', e); }
}

async function loadPersons(): Promise<void> {
  try {
    const res = await fetch('/api/persons');
    allPersons = persons = await res.json();
    renderPersonList();
    updatePersonSelect();
    updateFilterPerson();
  } catch (e) { console.error('Failed to load persons', e); }
}

function getPersonName(id: number): string {
  const p = allPersons.find((p: any) => p.id === id);
  return p ? p.name : '';
}

function getPerson(id: number): any {
  return allPersons.find((p: any) => p.id === id);
}

function renderEventList(): void {
  const list = document.getElementById('event-list');
  if (!list) return;
  const data = filteredEvents.length ? filteredEvents : events;
  if (!data.length) {
    list.innerHTML = '<tr><td colspan="6" class="text-center text-muted py-4">No events found</td></tr>';
    return;
  }
  list.innerHTML = data.map((e: any) => {
    const p = getPerson(e.person_id);
    return `<tr class="animate-in">
      <td class="ps-3 fw-bold font-monospace" style="font-size:0.85rem">${e.date}</td>
      <td><span class="fw-medium">${escapeHtml(e.title)}</span></td>
      <td>${e.location ? '<i class="fa-solid fa-location-dot me-1 text-muted" style="font-size:0.7rem"></i>' + escapeHtml(e.location) : '<span class="text-muted">—</span>'}</td>
      <td>${p ? '<span class="d-inline-flex align-items-center gap-1"><span class="color-dot" style="background:' + (p.color || '#7c3aed') + ';width:8px;height:8px"></span>' + escapeHtml(p.name) + '</span>' : '<span class="text-muted">—</span>'}</td>
      <td>${e.media_url ? '<span class="media-type-badge ' + e.media_type + '"><i class="fa-solid ' + getMediaIcon(e.media_type) + ' me-1"></i>' + e.media_type + '</span>' : '<span class="text-muted">—</span>'}</td>
      <td class="text-end pe-3">
        <button class="btn btn-sm btn-outline-primary me-1" onclick="editEvent(${e.id})" title="Edit"><i class="fa-solid fa-pen"></i></button>
        <button class="btn btn-sm btn-outline-danger" onclick="deleteEvent(${e.id})" title="Delete"><i class="fa-solid fa-trash"></i></button>
      </td>
    </tr>`;
  }).join('');
}

function getMediaIcon(t: string): string {
  if (t === 'video') return 'fa-video';
  if (t === 'audio') return 'fa-music';
  return 'fa-image';
}

function renderPersonList(): void {
  const container = document.getElementById('person-list');
  if (!container) return;
  const q = ((document.getElementById('person-search') as HTMLInputElement).value || '').toLowerCase();
  const filtered = persons.filter((p: any) => !q || p.name.toLowerCase().includes(q));

  if (!filtered.length) {
    container.innerHTML = '<div class="col-12"><div class="empty-state"><i class="fa-solid fa-users"></i><p>No people found</p></div></div>';
    return;
  }
  container.innerHTML = filtered.map((p: any) => {
    const initial = p.name ? p.name[0].toUpperCase() : '?';
    const eventCount = p.event_count || 0;
    return `<div class="col-md-6 col-lg-4">
      <div class="person-card" onclick="showPersonEvents(${p.id})">
        ${p.avatar_url
        ? '<img src="' + p.avatar_url + '" class="person-avatar" alt="">'
        : '<div class="person-avatar-placeholder" style="background:' + (p.color || '#7c3aed') + '">' + initial + '</div>'
      }
        <div class="person-info">
          <div class="name">${escapeHtml(p.name)}</div>
          <div class="meta">${p.bio ? escapeHtml(p.bio.substring(0, 50)) + (p.bio.length > 50 ? '...' : '') : ''}</div>
        </div>
        <div class="person-stats">
          <span class="count">${eventCount}</span>
          <span class="label">events</span>
        </div>
      </div>
    </div>`;
  }).join('');
}

function updatePersonSelect(): void {
  const select = document.getElementById('event-person') as HTMLSelectElement;
  if (!select) return;
  const val = select.value;
  select.innerHTML = '<option value="0">None</option>' + allPersons.map((p: any) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
  select.value = val;
}

function updateFilterPerson(): void {
  const select = document.getElementById('filter-person') as HTMLSelectElement;
  if (!select) return;
  select.innerHTML = '<option value="">All Persons</option>' + allPersons.map((p: any) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
}

let personSearchTimeout: any = null;
async function filterPersonSelect(): Promise<void> {
  const q = (document.getElementById('person-search-input') as HTMLInputElement).value.trim();
  clearTimeout(personSearchTimeout);
  personSearchTimeout = setTimeout(async () => {
    try {
      const select = document.getElementById('event-person') as HTMLSelectElement;
      const currentVal = select.value;
      const url = q ? `/api/persons?q=${encodeURIComponent(q)}` : '/api/persons';
      const res = await fetch(url);
      const searchedPersons = await res.json();
      select.innerHTML = '<option value="0">None</option>' + searchedPersons.map((p: any) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
      select.value = currentVal;
    } catch (e) { }
  }, 200);
}

function initLocationMap(): void {
  locationMap = L.map('location-map').setView([40.7128, -74.0060], 5);
  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', { attribution: '&copy; OpenStreetMap', maxZoom: 19 }).addTo(locationMap);
  locationMap.on('click', (e: any) => setMapMarker(e.latlng.lat, e.latlng.lng));
}

function setMapMarker(lat: number, lng: number): void {
  (document.getElementById('event-latitude') as HTMLInputElement).value = lat.toFixed(6);
  (document.getElementById('event-longitude') as HTMLInputElement).value = lng.toFixed(6);
  if (!locationMap) return;
  if (locationMarker) locationMap.removeLayer(locationMarker);
  locationMarker = L.marker([lat, lng]).addTo(locationMap);
  locationMap.setView([lat, lng], 13);
}

function useMyLocation(): void {
  if (!navigator.geolocation) { alert('Geolocation not supported'); return; }
  (document.getElementById('event-location') as HTMLInputElement).placeholder = 'Detecting...';
  navigator.geolocation.getCurrentPosition(
    async (pos) => {
      const lat = pos.coords.latitude;
      const lng = pos.coords.longitude;
      (document.getElementById('event-latitude') as HTMLInputElement).value = lat.toFixed(6);
      (document.getElementById('event-longitude') as HTMLInputElement).value = lng.toFixed(6);
      setMapMarker(lat, lng);
      try {
        const res = await fetch(
          `https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=${lat}&lon=${lng}`,
          { headers: { 'Accept-Language': 'en' } }
        );
        const data = await res.json();
        if (data.display_name && !(document.getElementById('event-location') as HTMLInputElement).value) {
          const parts = data.display_name.split(',');
          (document.getElementById('event-location') as HTMLInputElement).value = parts.slice(0, 3).join(',').trim();
        }
      } catch (e) { }
      (document.getElementById('event-location') as HTMLInputElement).placeholder = 'Type to search...';
    },
    (err) => { alert('Location failed: ' + err.message); (document.getElementById('event-location') as HTMLInputElement).placeholder = 'Type to search...'; },
    { enableHighAccuracy: true, timeout: 10000 }
  );
}

function initAutocomplete(inputId: string, dropdownId: string, field: string): void {
  const input = document.getElementById(inputId) as HTMLInputElement;
  const dropdown = document.getElementById(dropdownId) as HTMLElement;
  let timeout: any, selectedIdx = -1, items: string[] = [];

  input.addEventListener('input', function () {
    clearTimeout(timeout);
    const q = this.value.trim();
    if (q.length < 1) { dropdown.classList.remove('show'); return; }
    timeout = setTimeout(async () => {
      try {
        const res = await fetch('/api/autocomplete?field=' + field + '&q=' + encodeURIComponent(q));
        items = await res.json();
        if (!items.length) { dropdown.classList.remove('show'); return; }
        selectedIdx = -1;
        dropdown.innerHTML = items.map((v, i) =>
          '<div class="autocomplete-item" data-index="' + i + '" onclick="selectAutocomplete(\'' + inputId + '\',\'' + dropdownId + '\',\'' + escapeHtml(v) + '\')"><i class="fa-solid fa-' + (field === 'location' ? 'location-dot' : field === 'tag' ? 'tag' : 'file') + '"></i>' + escapeHtml(v) + '</div>'
        ).join('');
        dropdown.classList.add('show');
      } catch (e) { dropdown.classList.remove('show'); }
    }, 200);
  });

  input.addEventListener('keydown', function (e) {
    const items = dropdown.querySelectorAll('.autocomplete-item');
    if (e.key === 'ArrowDown') { e.preventDefault(); selectedIdx = Math.min(selectedIdx + 1, items.length - 1); highlightItem(items, selectedIdx); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); selectedIdx = Math.max(selectedIdx - 1, 0); highlightItem(items, selectedIdx); }
                else if (e.key === 'Enter' && selectedIdx >= 0 && items[selectedIdx]) { e.preventDefault(); (items[selectedIdx] as HTMLElement).click(); }
    else if (e.key === 'Escape') { dropdown.classList.remove('show'); }
  });

  document.addEventListener('click', function (e) {
    if (!input.contains(e.target as Node) && !dropdown.contains(e.target as Node)) dropdown.classList.remove('show');
  });
}

function highlightItem(items: NodeListOf<Element>, idx: number): void {
  items.forEach((item, i) => item.classList.toggle('active', i === idx));
}

function selectAutocomplete(inputId: string, dropdownId: string, value: string): void {
  (document.getElementById(inputId) as HTMLInputElement).value = value;
  document.getElementById(dropdownId)!.classList.remove('show');
}

function showPersonEvents(personId: number): void {
  viewingPersonId = personId;
  const person = getPerson(personId);
  if (!person) return;

  document.getElementById('events-tab')!.click();

  (document.getElementById('event-search') as HTMLInputElement).value = '';
  (document.getElementById('filter-person') as HTMLSelectElement).value = String(personId);
  (document.getElementById('filter-media') as HTMLSelectElement).value = '';
  applyFilters();

  const list = document.getElementById('event-list');
  if (!list) return;
  const initial = person.name ? person.name[0].toUpperCase() : '?';
  list.insertAdjacentHTML('afterbegin', `
    <tr id="person-banner">
      <td colspan="6" class="p-0">
        <div class="person-detail-header animate-in">
          ${person.avatar_url
      ? '<img src="' + person.avatar_url + '" class="avatar-lg" alt="">'
      : '<div class="avatar-placeholder-lg" style="background:' + (person.color || '#7c3aed') + '">' + initial + '</div>'
    }
          <div class="info">
            <h2>${escapeHtml(person.name)}</h2>
            <div class="sub">${person.bio || ''} ${person.birth_date ? '&middot; Born ' + person.birth_date : ''}</div>
          </div>
          <button class="close-btn" onclick="clearPersonFilter()"><i class="fa-solid fa-xmark"></i></button>
        </div>
      </td>
    </tr>
  `);
}

function clearPersonFilter(): void {
  viewingPersonId = null;
  (document.getElementById('filter-person') as HTMLSelectElement).value = '';
  applyFilters();
  const banner = document.getElementById('person-banner');
  if (banner) banner.remove();
}

function openEventModal(event?: any): void {
  if (!locationMap) initLocationMap();
  (document.getElementById('event-form') as HTMLFormElement).reset();
  (document.getElementById('event-id') as HTMLInputElement).value = '0';
  document.getElementById('eventModalLabel')!.textContent = 'Add Event';
  selectedTags = [];
  renderTags();
  clearEventMedia();

  if (event) {
    document.getElementById('eventModalLabel')!.textContent = 'Edit Event';
    (document.getElementById('event-id') as HTMLInputElement).value = event.id;
    (document.getElementById('event-title') as HTMLInputElement).value = event.title;
    (document.getElementById('event-desc') as HTMLTextAreaElement).value = event.description || '';
    (document.getElementById('event-date') as HTMLInputElement).value = event.date;
    (document.getElementById('event-location') as HTMLInputElement).value = event.location || '';
    (document.getElementById('event-person') as HTMLSelectElement).value = event.person_id || 0;
    (document.getElementById('event-media-type') as HTMLSelectElement).value = event.media_type || 'image';
    (document.getElementById('event-recurring') as HTMLSelectElement).value = event.recurring || '';
    (document.getElementById('event-user') as HTMLSelectElement).value = event.user_id || 0;
    document.getElementById('weather-display')!.textContent = '';
    delete (document.getElementById('weather-display') as any).dataset.weather;
    if (event.weather_data) {
      try {
        const weather = JSON.parse(event.weather_data);
        const weatherEl = document.getElementById('weather-display')!;
        weatherEl.innerHTML = '<i class="fa-solid fa-' + weather.icon + '"></i> ' + Math.round(weather.temperature) + '°C ' + weather.condition;
        (weatherEl as any).dataset.weather = event.weather_data;
      } catch (e) { }
    }
    (document.getElementById('event-tags-hidden') as HTMLInputElement).value = event.tags || '';
    selectedTags = event.tags ? event.tags.split(',').map((t: string) => t.trim()).filter((t: string) => t) : [];
    renderTags();
    (document.getElementById('event-latitude') as HTMLInputElement).value = event.latitude || '';
    (document.getElementById('event-longitude') as HTMLInputElement).value = event.longitude || '';
    if (event.media_url) {
      eventPhotoUrl = event.media_url;
      (document.getElementById('event-media-type') as HTMLSelectElement).value = event.media_type || 'image';
      document.getElementById('event-upload-zone')!.style.display = 'none';
      document.getElementById('event-media-preview')!.style.display = 'block';
      document.getElementById('event-media-name')!.textContent = event.media_url;
      const img = document.getElementById('event-media-img') as HTMLImageElement;
      const video = document.getElementById('event-media-video') as HTMLVideoElement;
      const audio = document.getElementById('event-media-audio') as HTMLElement;
      img.style.display = 'none'; video.style.display = 'none'; audio.style.display = 'none';
      if (event.media_type === 'video') { video.src = event.media_url; video.style.display = ''; }
      else if (event.media_type === 'audio') { audio.style.display = ''; }
      else { img.src = eventPhotoUrl; img.style.display = ''; }
    } else {
      clearEventMedia();
    }
    if (event.latitude && event.longitude) setMapMarker(event.latitude, event.longitude);
    else { if (locationMarker) locationMap.removeLayer(locationMarker); locationMarker = null; locationMap.setView([40.7128, -74.0060], 5); }
  } else {
    if (locationMarker && locationMap) locationMap.removeLayer(locationMarker);
    locationMarker = null;
    if (locationMap) locationMap.setView([40.7128, -74.0060], 5);
  }
  eventModal.show();
  setTimeout(() => { if (locationMap) locationMap.invalidateSize(); }, 200);
}

function editEvent(id: number): void {
  const e = events.find((e: any) => e.id === id);
  if (e) openEventModal(e);
}

async function deleteEvent(id: number): Promise<void> {
  if (!confirm('Delete this event?')) return;
  await ensureCSRF();
  await fetch('/api/events?id=' + id, { method: 'DELETE', headers: csrfHeaders() });
  if (viewingPersonId) clearPersonFilter();
  await loadEvents();
}

document.getElementById('event-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  await ensureCSRF();
  const title = (document.getElementById('event-title') as HTMLInputElement).value.trim();
  if (!title) { alert('Title is required'); return; }
  const weatherEl = document.getElementById('weather-display')!;
  const weatherData = (weatherEl as any).dataset.weather || '';
  const data = {
    id: parseInt((document.getElementById('event-id') as HTMLInputElement).value) || 0,
    title: title,
    description: (document.getElementById('event-desc') as HTMLTextAreaElement).value,
    date: (document.getElementById('event-date') as HTMLInputElement).value,
    location: (document.getElementById('event-location') as HTMLInputElement).value,
    person_id: parseInt((document.getElementById('event-person') as HTMLSelectElement).value) || 0,
    media_type: (document.getElementById('event-media-type') as HTMLSelectElement).value,
    media_url: eventPhotoUrl || '',
    tags: (document.getElementById('event-tags') as HTMLInputElement).value,
    latitude: parseFloat((document.getElementById('event-latitude') as HTMLInputElement).value) || null,
    longitude: parseFloat((document.getElementById('event-longitude') as HTMLInputElement).value) || null,
    weather_data: weatherData
  };
  const res = await fetch('/api/events', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  if (res.ok) {
    eventModal.hide();
    if (viewingPersonId) clearPersonFilter();
    await loadEvents();
  } else {
    const err = await res.json();
    alert('Failed to save event: ' + (err.error || ''));
  }
});

function openPersonModal(person?: any): void {
  (document.getElementById('person-form') as HTMLFormElement).reset();
  (document.getElementById('person-id') as HTMLInputElement).value = '0';
  (document.getElementById('person-color') as HTMLInputElement).value = '#7c3aed';
  document.getElementById('personModalLabel')!.textContent = 'Add Person';
  clearPersonAvatar();

  if (person) {
    document.getElementById('personModalLabel')!.textContent = 'Edit Person';
    (document.getElementById('person-id') as HTMLInputElement).value = person.id;
    (document.getElementById('person-name') as HTMLInputElement).value = person.name;
    (document.getElementById('person-avatar') as HTMLInputElement).value = person.avatar_url || '';
    (document.getElementById('person-bio') as HTMLTextAreaElement).value = person.bio || '';
    (document.getElementById('person-birth') as HTMLInputElement).value = person.birth_date || '';
    (document.getElementById('person-color') as HTMLInputElement).value = person.color || '#7c3aed';
    if (person.avatar_url) {
      (document.getElementById('person-avatar-img') as HTMLImageElement).src = person.avatar_url;
      document.getElementById('person-avatar-preview')!.style.display = 'block';
      document.getElementById('person-upload-zone')!.style.display = 'none';
    }
  }
  personModal.show();
}

const personUploadZone = document.getElementById('person-upload-zone');
if (personUploadZone) {
  ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(evt => {
    personUploadZone.addEventListener(evt, function (e) { e.preventDefault(); e.stopPropagation(); });
  });
  ['dragenter', 'dragover'].forEach(evt => {
    personUploadZone.addEventListener(evt, function () {
      (personUploadZone as HTMLElement).style.borderColor = 'var(--primary)';
      (personUploadZone as HTMLElement).style.background = 'var(--primary-glow)';
    });
  });
  ['dragleave', 'drop'].forEach(evt => {
    personUploadZone.addEventListener(evt, function () {
      (personUploadZone as HTMLElement).style.borderColor = 'var(--border)';
      (personUploadZone as HTMLElement).style.background = 'var(--bg)';
    });
  });
  personUploadZone.addEventListener('drop', async function (e) {
    const file = (e as DragEvent).dataTransfer!.files[0];
    if (!file) return;
    await uploadPersonAvatar(file);
  });
  personUploadZone.addEventListener('click', function (e) {
    if (!(e.target as HTMLElement).closest('button')) {
      document.getElementById('person-avatar-input')!.click();
    }
  });
}

document.getElementById('person-avatar-input')!.addEventListener('change', async function () {
  if ((this as HTMLInputElement).files![0]) await uploadPersonAvatar((this as HTMLInputElement).files![0]);
});

async function uploadPersonAvatar(file: File): Promise<void> {
  const result = document.getElementById('person-avatar-result')!;
  const preview = document.getElementById('person-avatar-preview')!;
  const img = document.getElementById('person-avatar-img') as HTMLImageElement;
  result.innerHTML = '<div class="text-center"><div class="spinner-border spinner-border-sm text-primary" role="status"></div> Uploading avatar...</div>';
  await ensureCSRF();
  const fd = new FormData();
  fd.append('image', file);
  fd.append('media_type', 'image');
  const res = await fetch('/api/upload', { method: 'POST', body: fd, headers: csrfHeaders() });
  const data = await res.json();
  if (res.ok) {
    (document.getElementById('person-avatar') as HTMLInputElement).value = data.url;
    img.src = data.thumbnail || data.url;
    preview.style.display = 'block';
    (document.getElementById('person-upload-zone') as HTMLElement).style.display = 'none';
    result.innerHTML = '';
  } else {
    result.innerHTML = '<div class="alert alert-danger py-1">Upload failed: ' + (data.error || '') + '</div>';
  }
}

function clearPersonAvatar(): void {
  (document.getElementById('person-avatar') as HTMLInputElement).value = '';
  document.getElementById('person-avatar-preview')!.style.display = 'none';
  (document.getElementById('person-avatar-img') as HTMLImageElement).src = '';
  (document.getElementById('person-avatar-input') as HTMLInputElement).value = '';
  document.getElementById('person-avatar-result')!.innerHTML = '';
  (document.getElementById('person-upload-zone') as HTMLElement).style.display = '';
}

async function deletePerson(id: number): Promise<void> {
  if (!confirm('Delete this person? Events linked will be unlinked.')) return;
  const res = await fetch('/api/persons?id=' + id, { method: 'DELETE', headers: csrfHeaders() });
  if (res.ok) {
    if (viewingPersonId === id) clearPersonFilter();
    await Promise.all([loadPersons(), loadEvents()]);
  } else alert('Failed to delete person');
}

document.getElementById('person-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    id: parseInt((document.getElementById('person-id') as HTMLInputElement).value) || 0,
    name: (document.getElementById('person-name') as HTMLInputElement).value,
    avatar_url: (document.getElementById('person-avatar') as HTMLInputElement).value,
    bio: (document.getElementById('person-bio') as HTMLTextAreaElement).value,
    birth_date: (document.getElementById('person-birth') as HTMLInputElement).value,
    color: (document.getElementById('person-color') as HTMLInputElement).value
  };
  const res = await fetch('/api/persons', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  if (res.ok) {
    personModal.hide();
    await Promise.all([loadPersons(), loadEvents()]);
  } else alert('Failed to save person');
});

function clearEventMedia(): void {
  eventPhotoUrl = '';
  document.getElementById('event-media-preview')!.style.display = 'none';
  (document.getElementById('event-media-img') as HTMLImageElement).src = '';
  (document.getElementById('event-media-img') as HTMLElement).style.display = 'none';
  (document.getElementById('event-media-video') as HTMLVideoElement).src = '';
  (document.getElementById('event-media-video') as HTMLElement).style.display = 'none';
  (document.getElementById('event-media-audio') as HTMLElement).style.display = 'none';
  document.getElementById('event-media-name')!.textContent = '';
  (document.getElementById('event-media-input') as HTMLInputElement).value = '';
  document.getElementById('event-media-result')!.innerHTML = '';
  document.getElementById('event-upload-zone')!.style.display = '';
  (document.getElementById('event-media-type') as HTMLSelectElement).value = 'image';
}

let inlinePersonSavedCallback: (() => void) | null = null;

function showInlinePersonForm(): void {
  (document.getElementById('inline-person-name') as HTMLInputElement).value = '';
  (document.getElementById('inline-person-color') as HTMLInputElement).value = '#7c3aed';
  document.getElementById('inline-person-result')!.innerHTML = '';
  document.getElementById('inline-person-form')!.style.display = '';
  document.getElementById('inline-person-name')!.focus();
}

function hideInlinePersonForm(): void {
  document.getElementById('inline-person-form')!.style.display = 'none';
  document.getElementById('inline-person-result')!.innerHTML = '';
}

async function saveInlinePerson(): Promise<void> {
  const name = (document.getElementById('inline-person-name') as HTMLInputElement).value.trim();
  if (!name) { document.getElementById('inline-person-result')!.innerHTML = '<span class="text-danger">Name is required</span>'; return; }
  const res = await fetch('/api/persons', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify({ id: 0, name: name, color: (document.getElementById('inline-person-color') as HTMLInputElement).value })
  });
  if (res.ok) {
    const person = await res.json();
    await loadPersons();
    (document.getElementById('event-person') as HTMLSelectElement).value = person.id;
    hideInlinePersonForm();
  } else {
    const err = await res.json();
    document.getElementById('inline-person-result')!.innerHTML = '<span class="text-danger">Failed: ' + (err.error || '') + '</span>';
  }
}

let selectedTags: string[] = [];

function renderTags(): void {
  const container = document.getElementById('selected-tags');
  if (!container) return;
  container.innerHTML = selectedTags.map(t =>
    '<span class="badge bg-primary d-inline-flex align-items-center gap-1" style="cursor:pointer" onclick="removeTag(\'' + escapeHtml(t) + '\')">' + escapeHtml(t) + ' <i class="fa-solid fa-xmark"></i></span>'
  ).join('');
  (document.getElementById('event-tags-hidden') as HTMLInputElement).value = selectedTags.join(', ');
}

function addTag(tag: string): void {
  tag = tag.trim().replace(/,$/, '').trim();
  if (!tag || selectedTags.includes(tag)) return;
  selectedTags.push(tag);
  renderTags();
  (document.getElementById('event-tags') as HTMLInputElement).value = '';
  document.getElementById('tags-autocomplete')!.classList.remove('show');
}

function removeTag(tag: string): void {
  selectedTags = selectedTags.filter(t => t !== tag);
  renderTags();
}

function initTagAutocomplete(): void {
  const input = document.getElementById('event-tags') as HTMLInputElement;
  const dropdown = document.getElementById('tags-autocomplete') as HTMLElement;
  let timeout: any, selectedIdx = -1, items: string[] = [];

  input.addEventListener('input', function () {
    clearTimeout(timeout);
    const q = this.value.trim().replace(/,.*$/, '');
    if (q.length < 1) { dropdown.classList.remove('show'); return; }
    timeout = setTimeout(async () => {
      try {
        const res = await fetch('/api/autocomplete?field=tag&q=' + encodeURIComponent(q));
        items = await res.json();
        selectedIdx = -1;
        let html = items.filter((v: string) => !selectedTags.includes(v)).map((v: string, i: number) =>
          '<div class="autocomplete-item" data-index="' + i + '" onclick="addTag(\'' + escapeHtml(v) + '\');document.getElementById(\'event-tags\').focus()"><i class="fa-solid fa-tag"></i>' + escapeHtml(v) + '</div>'
        ).join('');
        html += '<div class="autocomplete-item text-success fw-bold" onclick="addTag(document.getElementById(\'event-tags\').value);document.getElementById(\'event-tags\').focus()"><i class="fa-solid fa-plus"></i> Add "' + escapeHtml(q) + '"</div>';
        dropdown.innerHTML = html;
        dropdown.classList.add('show');
      } catch (e) { dropdown.classList.remove('show'); }
    }, 200);
  });

  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addTag(this.value);
    } else if (e.key === 'Backspace' && this.value === '' && selectedTags.length) {
      selectedTags.pop();
      renderTags();
    }
    const items = dropdown.querySelectorAll('.autocomplete-item');
    if (e.key === 'ArrowDown') { e.preventDefault(); selectedIdx = Math.min(selectedIdx + 1, items.length - 1); highlightItem(items, selectedIdx); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); selectedIdx = Math.max(selectedIdx - 1, 0); highlightItem(items, selectedIdx); }
                else if (e.key === 'Enter' && selectedIdx >= 0 && items[selectedIdx]) { e.preventDefault(); (items[selectedIdx] as HTMLElement).click(); }
    else if (e.key === 'Escape') { dropdown.classList.remove('show'); }
  });

  document.addEventListener('click', function (e) {
    if (!input.contains(e.target as Node) && !dropdown.contains(e.target as Node)) dropdown.classList.remove('show');
  });

  document.getElementById('add-tag-btn')!.addEventListener('click', function () {
    addTag(input.value);
    input.focus();
  });
}

function updateUploadField(): void {
  const type = (document.getElementById('media-type') as HTMLSelectElement).value;
  const fi = document.getElementById('media-file') as HTMLInputElement;
  const cam = document.getElementById('camera-input') as HTMLInputElement;
  const camBtn = document.getElementById('camera-btn') as HTMLElement;
  if (type === 'video') { fi.accept = 'video/mp4,video/webm,video/quicktime'; cam.style.display = 'none'; camBtn.style.display = 'none'; }
  else if (type === 'audio') { fi.accept = 'audio/mp3,audio/wav,audio/ogg'; cam.style.display = 'none'; camBtn.style.display = 'none'; }
  else { fi.accept = 'image/*'; cam.accept = 'image/*'; cam.style.display = 'none'; camBtn.style.display = ''; }
  (document.getElementById('media-file') as HTMLInputElement).value = '';
  document.getElementById('upload-preview')!.style.display = 'none';
  document.getElementById('upload-result')!.innerHTML = '';
}

function showUploadPreview(file: File): void {
  const preview = document.getElementById('upload-preview')!;
  const img = document.getElementById('upload-preview-img') as HTMLImageElement;
  const name = document.getElementById('upload-preview-name')!;
  preview.style.display = 'block';
  name.textContent = file.name + ' (' + formatFileSize(file.size) + ')';
  if (file.type.startsWith('image/')) {
    const reader = new FileReader();
    reader.onload = e => { img.src = e.target!.result as string; };
    reader.readAsDataURL(file);
  } else {
    img.src = '';
  }
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1048576).toFixed(1) + ' MB';
}

function openCamera(): void {
  const input = document.getElementById('camera-input') as HTMLInputElement;
  if (input) input.click();
}

const dropZone = document.getElementById('upload-drop-zone');
if (dropZone) {
  ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(evt => {
    dropZone.addEventListener(evt, function (e) { e.preventDefault(); e.stopPropagation(); });
  });
  ['dragenter', 'dragover'].forEach(evt => {
    dropZone.addEventListener(evt, function () {
      (dropZone as HTMLElement).style.borderColor = 'var(--primary)';
      (dropZone as HTMLElement).style.background = 'var(--primary-glow)';
    });
  });
  ['dragleave', 'drop'].forEach(evt => {
    dropZone.addEventListener(evt, function () {
      (dropZone as HTMLElement).style.borderColor = 'var(--border)';
      (dropZone as HTMLElement).style.background = 'var(--bg)';
    });
  });
  dropZone.addEventListener('drop', function (e) {
    const file = (e as DragEvent).dataTransfer!.files[0];
    if (!file) return;
    (document.getElementById('media-file') as HTMLInputElement).files = (e as DragEvent).dataTransfer!.files;
    showUploadPreview(file);
  });
  dropZone.addEventListener('click', function (e) {
    if (!(e.target as HTMLElement).closest('button')) {
      document.getElementById('media-file')!.click();
    }
  });
}

document.getElementById('media-file')!.addEventListener('change', function () {
  if ((this as HTMLInputElement).files![0]) showUploadPreview((this as HTMLInputElement).files![0]);
});

document.getElementById('camera-input')!.addEventListener('change', function () {
  if ((this as HTMLInputElement).files![0]) {
    const dt = new DataTransfer();
    dt.items.add((this as HTMLInputElement).files![0]);
    (document.getElementById('media-file') as HTMLInputElement).files = dt.files;
    showUploadPreview((this as HTMLInputElement).files![0]);
  }
});

document.getElementById('upload-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const type = (document.getElementById('media-type') as HTMLSelectElement).value;
  const fileInput = document.getElementById('media-file') as HTMLInputElement;
  if (!fileInput.files![0]) { alert('Select a file'); return; }
  await ensureCSRF();
  const fd = new FormData();
  fd.append(type, fileInput.files![0]);
  fd.append('media_type', type);
  const el = document.getElementById('upload-result')!;
  el.innerHTML = '<div class="text-center"><div class="spinner-border text-primary" role="status"></div><p class="mt-2">Uploading...</p></div>';
  const res = await fetch('/api/upload', { method: 'POST', body: fd, headers: csrfHeaders() });
  const data = await res.json();
  if (res.ok) {
    let html = '<div class="alert alert-success"><strong>Uploaded!</strong><br>URL: <code class="user-select-all">' + data.url + '</code>';
    if (data.thumbnail) html += '<br>Thumbnail: <code class="user-select-all">' + data.thumbnail + '</code>';
    html += '</div>';
    if (data.thumbnail) html += '<div class="text-center mt-2"><img src="' + data.thumbnail + '" class="img-fluid rounded shadow-sm" style="max-height:300px" alt="preview"></div>';
    el.innerHTML = html;
  } else el.innerHTML = '<div class="alert alert-danger">Upload failed: ' + (data.error || '') + '</div>';
});

const eventUploadZone = document.getElementById('event-upload-zone');
if (eventUploadZone) {
  ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(evt => {
    eventUploadZone.addEventListener(evt, function (e) { e.preventDefault(); e.stopPropagation(); });
  });
  ['dragenter', 'dragover'].forEach(evt => {
    eventUploadZone.addEventListener(evt, function () {
      (eventUploadZone as HTMLElement).style.borderColor = 'var(--primary)';
      (eventUploadZone as HTMLElement).style.background = 'var(--primary-glow)';
    });
  });
  ['dragleave', 'drop'].forEach(evt => {
    eventUploadZone.addEventListener(evt, function () {
      (eventUploadZone as HTMLElement).style.borderColor = 'var(--border)';
      (eventUploadZone as HTMLElement).style.background = 'var(--bg)';
    });
  });
  eventUploadZone.addEventListener('drop', async function (e) {
    const file = (e as DragEvent).dataTransfer!.files[0];
    if (!file) return;
    await uploadEventMedia(file);
  });
  eventUploadZone.addEventListener('click', function (e) {
    if (!(e.target as HTMLElement).closest('button')) {
      document.getElementById('event-media-input')!.click();
    }
  });
}

document.getElementById('event-media-input')!.addEventListener('change', async function () {
  if ((this as HTMLInputElement).files![0]) await uploadEventMedia((this as HTMLInputElement).files![0]);
});

async function uploadEventMedia(file: File): Promise<void> {
  const result = document.getElementById('event-media-result')!;
  const preview = document.getElementById('event-media-preview')!;
  const img = document.getElementById('event-media-img') as HTMLImageElement;
  const video = document.getElementById('event-media-video') as HTMLVideoElement;
  const audio = document.getElementById('event-media-audio') as HTMLElement;
  const nameEl = document.getElementById('event-media-name')!;
  result.innerHTML = '<div class="text-center"><div class="spinner-border spinner-border-sm text-primary" role="status"></div> Uploading...</div>';
  await ensureCSRF();
  const fd = new FormData();
  let mediaType = 'image';
  if (file.type.startsWith('video/')) mediaType = 'video';
  else if (file.type.startsWith('audio/')) mediaType = 'audio';
  fd.append(mediaType, file);
  fd.append('media_type', mediaType);
  const res = await fetch('/api/upload', { method: 'POST', body: fd, headers: csrfHeaders() });
  const data = await res.json();
  if (res.ok) {
    eventPhotoUrl = data.url;
    (document.getElementById('event-media-type') as HTMLSelectElement).value = mediaType;
    img.style.display = 'none';
    video.style.display = 'none';
    audio.style.display = 'none';
    if (mediaType === 'video') {
      video.src = data.url;
      video.style.display = '';
    } else if (mediaType === 'audio') {
      audio.style.display = '';
    } else {
      img.src = data.url;
      img.style.display = '';
    }
    nameEl.textContent = file.name;
    preview.style.display = 'block';
    (document.getElementById('event-upload-zone') as HTMLElement).style.display = 'none';
    result.innerHTML = '';
  } else {
    result.innerHTML = '<div class="alert alert-danger py-1">Upload failed: ' + (data.error || '') + '</div>';
  }
}

async function loadGotifyConfig(): Promise<void> {
  try {
    const res = await fetch('/api/gotify/config');
    const cfg = await res.json();
    (document.getElementById('gotify-url') as HTMLInputElement).value = cfg.url || '';
    (document.getElementById('gotify-token') as HTMLInputElement).value = cfg.token || '';
    (document.getElementById('gotify-enabled') as HTMLInputElement).checked = cfg.enabled || false;
  } catch (e) { }
}

document.getElementById('gotify-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    url: (document.getElementById('gotify-url') as HTMLInputElement).value,
    token: (document.getElementById('gotify-token') as HTMLInputElement).value,
    enabled: (document.getElementById('gotify-enabled') as HTMLInputElement).checked
  };
  const res = await fetch('/api/gotify/config', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  const result = await res.json();
  document.getElementById('gotify-result')!.innerHTML = res.ok
    ? '<div class="alert alert-success">Gotify settings saved.</div>'
    : '<div class="alert alert-danger">Error: ' + (result.error || '') + '</div>';
});

async function testGotify(): Promise<void> {
  const el = document.getElementById('gotify-result')!;
  el.innerHTML = '<div class="alert alert-info">Sending test...</div>';
  await ensureCSRF();
  const res = await fetch('/api/gotify/test', { method: 'POST', headers: csrfHeaders() });
  const data = await res.json();
  el.innerHTML = res.ok
    ? '<div class="alert alert-success">Test notification sent!</div>'
    : '<div class="alert alert-danger">Error: ' + (data.error || '') + '</div>';
}

let csrfToken = '';

async function ensureCSRF(): Promise<string> {
  if (csrfToken) return csrfToken;
  try {
    const res = await fetch('/api/csrf-token');
    if (res.ok) {
      const data = await res.json();
      csrfToken = data.token;
    }
  } catch (e) { }
  return csrfToken;
}

function csrfHeaders(contentType?: string): Record<string, string> {
  const h: Record<string, string> = {};
  if (csrfToken) h['X-CSRF-Token'] = csrfToken;
  if (contentType) h['Content-Type'] = contentType;
  return h;
}

function logout(): void {
  fetch('/api/logout', { method: 'POST' }).then(() => window.location.href = '/login.html');
}

async function loadMemoriesConfig(): Promise<void> {
  try {
    const res = await fetch('/api/memories/config');
    const cfg = await res.json();
    (document.getElementById('memories-enabled') as HTMLInputElement).checked = cfg.enabled || false;
    (document.getElementById('memories-days') as HTMLInputElement).value = cfg.days_window || 3;
    document.getElementById('memories-days-label')!.textContent = cfg.days_window || 3;
    (document.getElementById('memories-email') as HTMLInputElement).checked = cfg.email_enabled || false;
  } catch (e) { }
}

document.getElementById('memories-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    enabled: (document.getElementById('memories-enabled') as HTMLInputElement).checked,
    days_window: parseInt((document.getElementById('memories-days') as HTMLInputElement).value) || 3,
    email_enabled: (document.getElementById('memories-email') as HTMLInputElement).checked
  };
  const res = await fetch('/api/memories/config', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  const result = await res.json();
  document.getElementById('memories-config-result')!.innerHTML = res.ok
    ? '<div class="alert alert-success">Memories settings saved.</div>'
    : '<div class="alert alert-danger">Error: ' + (result.error || '') + '</div>';
});

async function loadMemories(): Promise<void> {
  const list = document.getElementById('memories-list')!;
  list.innerHTML = '<div class="text-center text-muted py-4"><i class="fa-solid fa-spinner fa-spin me-2"></i>Loading...</div>';
  try {
    const res = await fetch('/api/memories');
    const memories = await res.json();
    if (!memories.length) {
      list.innerHTML = '<div class="text-center text-muted py-4"><i class="fa-solid fa-clock-rotate-left me-2" style="font-size:2rem"></i><p class="mt-2">No memories for today. Add some events from past years to see them here.</p></div>';
      return;
    }
    memories.sort((a: any, b: any) => a.years_ago - b.years_ago);
    list.innerHTML = memories.map((m: any) => {
      const icon = m.media_type === 'video' ? 'fa-video' : m.media_type === 'audio' ? 'fa-music' : 'fa-image';
      return '<div class="d-flex align-items-start gap-3 p-3 border-bottom animate-in">'
        + '<div class="flex-shrink-0" style="width:48px;height:48px;background:var(--primary-glow);border-radius:8px;display:flex;align-items:center;justify-content:center;color:var(--primary);font-weight:700;font-size:1.1rem">' + m.years_ago + '</div>'
        + '<div class="flex-grow-1" style="min-width:0">'
        + '<div class="fw-semibold">' + escapeHtml(m.title) + '</div>'
        + '<div class="text-muted" style="font-size:0.85rem">' + m.years_ago + ' year' + (m.years_ago > 1 ? 's' : '') + ' ago &middot; ' + m.date + '</div>'
        + (m.location ? '<div class="text-muted" style="font-size:0.8rem"><i class="fa-solid fa-location-dot me-1"></i>' + escapeHtml(m.location) + '</div>' : '')
        + '</div>'
        + (m.thumbnail ? '<img src="' + m.thumbnail + '" class="rounded flex-shrink-0" style="width:64px;height:64px;object-fit:cover" alt="">'
          : m.media_url ? '<i class="fa-solid ' + icon + '" style="font-size:1.5rem;color:var(--text-muted)"></i>' : '')
        + '</div>';
    }).join('');
  } catch (e) {
    list.innerHTML = '<div class="alert alert-danger">Failed to load memories</div>';
  }
}

async function sendMemoriesNow(): Promise<void> {
  const evt = (window as any).event;
  const btn = evt.target as HTMLButtonElement;
  const origText = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin me-1"></i> Sending...';
  await ensureCSRF();
  const res = await fetch('/api/memories/send', { method: 'POST', headers: csrfHeaders() });
  const data = await res.json();
  document.getElementById('memories-config-result')!.innerHTML = res.ok
    ? '<div class="alert alert-success">' + (data.message || 'Sent!') + '</div>'
    : '<div class="alert alert-danger">Error: ' + (data.error || '') + '</div>';
  btn.disabled = false;
  btn.innerHTML = origText;
}

async function loadEmailConfig(): Promise<void> {
  try {
    const res = await fetch('/api/email/config');
    const cfg = await res.json();
    (document.getElementById('email-host') as HTMLInputElement).value = cfg.smtp_host || '';
    (document.getElementById('email-port') as HTMLInputElement).value = cfg.smtp_port || 587;
    (document.getElementById('email-user') as HTMLInputElement).value = cfg.smtp_user || '';
    (document.getElementById('email-pass') as HTMLInputElement).value = cfg.smtp_pass || '';
    (document.getElementById('email-from') as HTMLInputElement).value = cfg.from_addr || '';
    (document.getElementById('email-to') as HTMLInputElement).value = cfg.to_addr || '';
  } catch (e) { }
}

document.getElementById('email-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    smtp_host: (document.getElementById('email-host') as HTMLInputElement).value,
    smtp_port: parseInt((document.getElementById('email-port') as HTMLInputElement).value) || 587,
    smtp_user: (document.getElementById('email-user') as HTMLInputElement).value,
    smtp_pass: (document.getElementById('email-pass') as HTMLInputElement).value,
    from_addr: (document.getElementById('email-from') as HTMLInputElement).value,
    to_addr: (document.getElementById('email-to') as HTMLInputElement).value
  };
  const res = await fetch('/api/email/config', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  const result = await res.json();
  document.getElementById('email-result')!.innerHTML = res.ok
    ? '<div class="alert alert-success">Email settings saved.</div>'
    : '<div class="alert alert-danger">Error: ' + (result.error || '') + '</div>';
});

async function testEmailConfig(): Promise<void> {
  const el = document.getElementById('email-result')!;
  el.innerHTML = '<div class="alert alert-info">Sending test email...</div>';
  await ensureCSRF();
  const res = await fetch('/api/email/test', { method: 'POST', headers: csrfHeaders() });
  const data = await res.json();
  el.innerHTML = res.ok
    ? '<div class="alert alert-success">Test email sent successfully!</div>'
    : '<div class="alert alert-danger">Error: ' + (data.error || '') + '</div>';
}

let adminUsers: any[] = [];

async function loadUsers(): Promise<void> {
  try {
    const res = await fetch('/api/users');
    adminUsers = await res.json();
    renderUserList();
    updateUserSelect();
  } catch (e) { console.error('Failed to load users', e); }
}

function renderUserList(): void {
  const container = document.getElementById('user-list');
  if (!container) return;
  if (!adminUsers.length) {
    container.innerHTML = '<div class="col-12"><div class="empty-state"><i class="fa-solid fa-users"></i><p>No users yet</p></div></div>';
    return;
  }
  container.innerHTML = adminUsers.map((u: any) => `
    <div class="col-md-6 col-lg-4">
      <div class="person-card">
        <div class="person-avatar-placeholder" style="background:${u.color || '#7c3aed'}">
          <i class="fa-solid fa-user" style="color:white;font-size:1.2rem"></i>
        </div>
        <div class="person-info">
          <div class="name">${escapeHtml(u.display_name || u.username)}</div>
          <div class="meta">@${escapeHtml(u.username)} ${u.event_count ? '· ' + u.event_count + ' events' : ''}</div>
        </div>
        <div class="person-stats">
          <button class="btn btn-sm btn-outline-primary" onclick="openUserModal(${u.id})"><i class="fa-solid fa-pen"></i></button>
          <button class="btn btn-sm btn-outline-danger ms-1" onclick="deleteAdminUser(${u.id})"><i class="fa-solid fa-trash"></i></button>
        </div>
      </div>
    </div>
  `).join('');
}

function updateUserSelect(): void {
  const select = document.getElementById('event-user') as HTMLSelectElement;
  if (!select) return;
  const val = select.value;
  select.innerHTML = '<option value="0">Default</option>' + adminUsers.map((u: any) => `<option value="${u.id}">${escapeHtml(u.display_name || u.username)}</option>`).join('');
  select.value = val;
}

function openUserModal(userId?: number): void {
  (document.getElementById('user-form') as HTMLFormElement).reset();
  (document.getElementById('user-id') as HTMLInputElement).value = '0';
  (document.getElementById('user-color') as HTMLInputElement).value = '#7c3aed';
  document.getElementById('userModalLabel')!.textContent = 'Add User';
  if (userId) {
    const u = adminUsers.find((u: any) => u.id === userId);
    if (u) {
      document.getElementById('userModalLabel')!.textContent = 'Edit User';
      (document.getElementById('user-id') as HTMLInputElement).value = u.id;
      (document.getElementById('user-username') as HTMLInputElement).value = u.username;
      (document.getElementById('user-display-name') as HTMLInputElement).value = u.display_name || '';
      (document.getElementById('user-color') as HTMLInputElement).value = u.color || '#7c3aed';
    }
  }
  new bootstrap.Modal(document.getElementById('userModal')).show();
}

document.getElementById('user-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    id: parseInt((document.getElementById('user-id') as HTMLInputElement).value) || 0,
    username: (document.getElementById('user-username') as HTMLInputElement).value,
    display_name: (document.getElementById('user-display-name') as HTMLInputElement).value,
    color: (document.getElementById('user-color') as HTMLInputElement).value
  };
  const res = await fetch('/api/users', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  if (res.ok) {
    bootstrap.Modal.getInstance(document.getElementById('userModal'))!.hide();
    await loadUsers();
  } else alert('Failed to save user');
});

async function deleteAdminUser(id: number): Promise<void> {
  if (!confirm('Delete this user? Their events will be unlinked.')) return;
  await ensureCSRF();
  const res = await fetch('/api/users?id=' + id, { method: 'DELETE', headers: csrfHeaders() });
  if (res.ok) await loadUsers();
  else alert('Failed to delete user');
}

async function loadOllamaConfig(): Promise<void> {
  try {
    const res = await fetch('/api/ollama/config');
    const cfg = await res.json();
    (document.getElementById('ollama-url') as HTMLInputElement).value = cfg.url || 'http://localhost:11434';
    (document.getElementById('ollama-model') as HTMLInputElement).value = cfg.model || 'llama3.2';
    (document.getElementById('ollama-enabled') as HTMLInputElement).checked = cfg.enabled || false;
  } catch (e) { }
}

document.getElementById('ollama-form')!.addEventListener('submit', async (e) => {
  e.preventDefault();
  const data = {
    url: (document.getElementById('ollama-url') as HTMLInputElement).value,
    model: (document.getElementById('ollama-model') as HTMLInputElement).value,
    enabled: (document.getElementById('ollama-enabled') as HTMLInputElement).checked
  };
  const res = await fetch('/api/ollama/config', {
    method: 'POST',
    headers: csrfHeaders('application/json'),
    body: JSON.stringify(data)
  });
  document.getElementById('ollama-result')!.innerHTML = res.ok
    ? '<div class="alert alert-success">AI settings saved.</div>'
    : '<div class="alert alert-danger">Failed to save.</div>';
});

async function fetchEventWeather(): Promise<void> {
  const lat = (document.getElementById('event-latitude') as HTMLInputElement).value;
  const lng = (document.getElementById('event-longitude') as HTMLInputElement).value;
  const date = (document.getElementById('event-date') as HTMLInputElement).value;
  const id = parseInt((document.getElementById('event-id') as HTMLInputElement).value) || 0;
  if (!lat || !lng || !date) { alert('Set date, latitude, and longitude first'); return; }
  const el = document.getElementById('weather-display')!;
  el.textContent = 'Fetching...';
  try {
    const res = await fetch('/api/weather/fetch', {
      method: 'POST',
      headers: csrfHeaders('application/json'),
      body: JSON.stringify({ event_id: id, latitude: parseFloat(lat), longitude: parseFloat(lng), date: date })
    });
    const data = await res.json();
    if (res.ok) {
      el.innerHTML = '<i class="fa-solid fa-' + data.icon + '"></i> ' + Math.round(data.temperature) + '°C ' + data.condition;
      (el as any).dataset.weather = JSON.stringify(data);
    } else {
      el.textContent = 'Weather: ' + (data.error || 'unavailable');
    }
  } catch (e) { el.textContent = 'Weather fetch failed'; }
}

async function autoTagEvent(): Promise<void> {
  const title = (document.getElementById('event-title') as HTMLInputElement).value;
  const desc = (document.getElementById('event-desc') as HTMLTextAreaElement).value;
  const loc = (document.getElementById('event-location') as HTMLInputElement).value;
  if (!title) { alert('Enter a title first'); return; }
  const evt = (window as any).event;
  const btn = evt.target as HTMLButtonElement;
  const orig = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Analyzing...';
  try {
    const res = await fetch('/api/auto-tag', {
      method: 'POST',
      headers: csrfHeaders('application/json'),
      body: JSON.stringify({ title, description: desc, location: loc })
    });
    const data = await res.json();
    if (res.ok && data.tags) {
      data.tags.forEach((t: string) => addTag(t));
    } else {
      alert('Auto-tag failed: ' + (data.error || 'unknown'));
    }
  } catch (e) { alert('Auto-tag failed'); }
  btn.disabled = false;
  btn.innerHTML = orig;
}

async function loadBackups(): Promise<void> {
  const tbody = document.getElementById('backup-list')!;
  try {
    const res = await fetch('/api/backups', { headers: csrfHeaders() });
    const backups = await res.json();
    tbody.innerHTML = backups.map((b: any) => '<tr><td>' + escapeHtml(b.name) + '</td><td>' + formatSize(b.size) + '</td><td>' + new Date(b.date).toLocaleString() + '</td></tr>').join('');
  } catch (e) { tbody.innerHTML = '<tr><td colspan="3" class="text-muted">Failed to load backups</td></tr>'; }
}

async function createBackup(): Promise<void> {
  const evt = (window as any).event;
  const btn = evt.target as HTMLButtonElement;
  const orig = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Backing up...';
  try {
    const res = await fetch('/api/backup', { method: 'POST', headers: csrfHeaders('application/json') });
    const data = await res.json();
    if (res.ok) {
      document.getElementById('backup-result')!.innerHTML = '<div class="alert alert-success mb-0">Backup created successfully</div>';
      loadBackups();
    } else {
      document.getElementById('backup-result')!.innerHTML = '<div class="alert alert-danger mb-0">Backup failed: ' + (data.error || 'unknown') + '</div>';
    }
  } catch (e) { document.getElementById('backup-result')!.innerHTML = '<div class="alert alert-danger mb-0">Backup failed</div>'; }
  btn.disabled = false;
  btn.innerHTML = orig;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1048576).toFixed(1) + ' MB';
}

function escapeHtml(text: string): string {
  if (!text) return '';
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

init();

(window as any).logout = logout;
(window as any).loadEvents = loadEvents;
(window as any).loadPersons = loadPersons;
(window as any).openEventModal = openEventModal;
(window as any).editEvent = editEvent;
(window as any).deleteEvent = deleteEvent;
(window as any).showPersonEvents = showPersonEvents;
(window as any).clearPersonFilter = clearPersonFilter;
(window as any).openPersonModal = openPersonModal;
(window as any).deletePerson = deletePerson;
(window as any).useMyLocation = useMyLocation;
(window as any).openCamera = openCamera;
(window as any).sendMemoriesNow = sendMemoriesNow;
(window as any).loadMemories = loadMemories;
(window as any).testGotify = testGotify;
(window as any).testEmailConfig = testEmailConfig;
(window as any).createBackup = createBackup;
(window as any).openUserModal = openUserModal;
(window as any).deleteAdminUser = deleteAdminUser;
(window as any).fetchEventWeather = fetchEventWeather;
(window as any).autoTagEvent = autoTagEvent;
(window as any).showInlinePersonForm = showInlinePersonForm;
(window as any).saveInlinePerson = saveInlinePerson;
(window as any).hideInlinePersonForm = hideInlinePersonForm;
(window as any).clearEventMedia = clearEventMedia;
(window as any).clearPersonAvatar = clearPersonAvatar;
(window as any).selectAutocomplete = selectAutocomplete;
(window as any).addTag = addTag;
(window as any).removeTag = removeTag;
(window as any).filterPersonSelect = filterPersonSelect;
(window as any).debouncedSearch = debouncedSearch;
(window as any).applyFilters = applyFilters;
(window as any).updateUploadField = updateUploadField;
