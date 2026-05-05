"use strict";
let currentYear = new Date().getFullYear();
let currentMonth = 0;
let events = [];
let contributions = {};
let galleryPage = 1;
const galleryLimit = 12;
let map = null;
let markers = [];
let users = [];
let isAuthenticated = false;
const monthNames = [
    'January', 'February', 'March', 'April', 'May', 'June',
    'July', 'August', 'September', 'October', 'November', 'December'
];
const themeToggle = document.getElementById('theme-toggle');
const themeIcon = themeToggle?.querySelector('i');
function setTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
    if (themeIcon) {
        themeIcon.className = theme === 'dark' ? 'fa-solid fa-sun' : 'fa-solid fa-moon';
    }
}
function initTheme() {
    const savedTheme = (localStorage.getItem('theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'));
    setTheme(savedTheme);
    themeToggle?.addEventListener('click', () => {
        const current = document.documentElement.getAttribute('data-theme');
        setTheme(current === 'dark' ? 'light' : 'dark');
    });
}
function changeYear(year) {
    currentYear = year;
    const yearEl = document.getElementById('current-year');
    if (yearEl)
        yearEl.textContent = String(year);
    document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.remove('btn-primary'));
    document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.add('btn-outline-primary'));
    const btn = document.querySelector(`.year-selector .btn[data-year="${year}"]`);
    if (btn) {
        btn.classList.remove('btn-outline-primary');
        btn.classList.add('btn-primary');
    }
    loadData();
}
function searchEvents() {
    const input = document.getElementById('search-input');
    if (input?.value) {
        window.location.href = '?q=' + encodeURIComponent(input.value);
    }
}
function filterMonth(month) {
    currentMonth = month;
    const buttons = document.querySelectorAll('.month-filter .btn');
    buttons.forEach((btn, i) => {
        btn.classList.toggle('active', i === month);
        btn.classList.toggle('btn-dark', i === month);
        btn.classList.toggle('btn-outline-dark', i !== month);
    });
    renderTimeline();
    renderGallery();
    renderMap();
}
async function loadContributions() {
    try {
        const res = await fetch('/api/contributions?year=' + currentYear);
        if (res.status === 401 && events.length > 0) {
            contributions = {};
            events.forEach(e => {
                if (e.date && e.date.startsWith(String(currentYear))) {
                    contributions[e.date] = (contributions[e.date] || 0) + 1;
                }
            });
        } else if (!res.ok) {
            contributions = {};
        }
        else {
            contributions = await res.json();
        }
        renderContributionGraph();
        updateStats();
    }
    catch (err) {
        console.error('Failed to load contributions:', err);
    }
}
async function loadUsers() {
    try {
        const res = await fetch('/api/users');
        if (res.ok) {
            users = await res.json();
        } else {
            users = [];
        }
        if (!Array.isArray(users))
            users = [];
    }
    catch (e) {
        users = [];
    }
}

function renderContributionGraph() {
    const graph = document.getElementById('contribution-graph');
    if (!graph)
        return;
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
function updateStats() {
    const eventList = Array.isArray(events) ? events : [];
    const filtered = eventList.length > 0
        ? (currentMonth > 0 ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth) : eventList)
        : [];
    const totalEl = document.getElementById('total-events');
    if (totalEl)
        totalEl.textContent = filtered.length + ' events';
    const images = filtered.filter(e => e.media_type === 'image').length;
    const videos = filtered.filter(e => e.media_type === 'video').length;
    const audio = filtered.filter(e => e.media_type === 'audio').length;
    const locations = new Set(filtered.map(e => e.location).filter(l => l)).size;
    const statImages = document.getElementById('stat-images');
    const statVideos = document.getElementById('stat-videos');
    const statAudio = document.getElementById('stat-audio');
    const statLocations = document.getElementById('stat-locations');
    if (statImages)
        statImages.textContent = String(images);
    if (statVideos)
        statVideos.textContent = String(videos);
    if (statAudio)
        statAudio.textContent = String(audio);
    if (statLocations)
        statLocations.textContent = String(locations);
}
async function loadEvents() {
    try {
        let url = '/api/events?year=' + currentYear;
        if (currentMonth > 0) {
            url += '&month=' + String(currentMonth).padStart(2, '0');
        }
        const res = await fetch(url);
        if (res.status === 401) {
            isAuthenticated = false;
            const pubRes = await fetch('/api/public?year=' + currentYear);
            if (pubRes.ok) {
                events = await pubRes.json();
            } else {
                events = [];
            }
        } else if (!res.ok) {
            events = [];
        } else {
            isAuthenticated = true;
            events = await res.json();
        }
        if (!Array.isArray(events))
            events = [];
        renderTimeline();
        renderGallery();
        renderMap();
        renderCalendar();
    }
    catch (err) {
        console.error('Failed to load events:', err);
    }
}

async function loadData() {
    await Promise.all([loadEvents(), loadContributions(), loadUsers()]);
    populateYearButtons();
    galleryPage = 1;
}
function populateYearButtons() {
    const container = document.getElementById('year-buttons');
    if (!container)
        return;
    const years = new Set();
    events.forEach(e => {
        const y = parseInt(e.date.slice(0, 4));
        if (!isNaN(y))
            years.add(y);
    });
    years.add(currentYear);
    const sorted = Array.from(years).sort((a, b) => b - a);
    container.innerHTML = sorted.map(y => `<button class="btn ${y === currentYear ? 'btn-primary' : 'btn-outline-primary'}" data-year="${y}" onclick="changeYear(${y})">${y}</button>`).join('');
}
function renderTimeline() {
    const container = document.getElementById('timeline-container');
    if (!container)
        return;
    const eventList = Array.isArray(events) ? events : [];
    if (eventList.length === 0) {
        container.innerHTML = `
      <div class="text-center text-muted py-5">
        <i class="fa-solid fa-clock fa-3x mb-3"></i>
        <p>No events found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    const filteredEvents = currentMonth > 0
        ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
        : eventList;
    if (filteredEvents.length === 0) {
        container.innerHTML = `
      <div class="text-center text-muted py-5">
        <i class="fa-solid fa-clock fa-3x mb-3"></i>
        <p>No events found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    let currentMonthIdx = -1;
    let html = '';
    filteredEvents.forEach((e, index) => {
        const eventDate = new Date(e.date);
        const month = eventDate.getMonth();
        if (month !== currentMonthIdx) {
            currentMonthIdx = month;
            html += `
        <div class="timeline-month-label">
          <span class="badge bg-primary">${monthNames[month]}</span>
        </div>
      `;
        }
        const mediaIcon = getMediaIcon(e.media_type);
        const hasMedia = e.media_url;
        const tagList = e.tags ? e.tags.split(',').map(t => t.trim()).filter(t => t) : [];
        let weatherHtml = '';
        if (e.weather_data) {
            try {
                const w = JSON.parse(e.weather_data);
                weatherHtml = `<span class="weather-badge ms-2"><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</span>`;
            }
            catch (_) { }
        }
        let userHtml = '';
        if (e.user_id && users.length) {
            const u = users.find(u => u.id === e.user_id);
            if (u) {
                userHtml = `<span class="user-badge ms-1" style="background:${u.color || '#7c3aed'}"><i class="fa-solid fa-user"></i> ${escapeHtml(u.display_name || u.username)}</span>`;
            }
        }
        const recurringBadge = e.recurring ? `<span class="badge bg-info ms-1"><i class="fa-solid fa-rotate"></i> ${e.recurring}</span>` : '';
        html += `
      <div class="timeline-item ${index % 2 === 0 ? 'left' : 'right'}" id="event-${e.id}">
        <div class="timeline-content" onclick="${hasMedia ? 'showMedia(' + e.id + ')' : ''}">
          <div class="timeline-date">${formatDate(e.date)} ${recurringBadge}</div>
          <div class="timeline-title">${escapeHtml(e.title)} ${weatherHtml}</div>
          <div class="timeline-location"><i class="fa-solid fa-location-dot me-1"></i>${escapeHtml(e.location)} ${userHtml}</div>
          ${tagList.length > 0 ? '<div class="timeline-people mb-2"><i class="fa-solid fa-tags me-1"></i>' + tagList.map(t => '<span class="badge bg-secondary me-1">' + escapeHtml(t) + '</span>').join('') + '</div>' : ''}
          ${e.description ? '<div class="timeline-desc md-content">' + renderMarkdown(e.description) + '</div>' : ''}
          ${hasMedia ? '<div class="timeline-media-badge"><i class="' + mediaIcon + '"></i> ' + e.media_type + '</div>' : ''}
        </div>
      </div>
    `;
    });
    container.innerHTML = html;
}
function renderMarkdown(text) {
    if (!text)
        return '';
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
function getMediaIcon(mediaType) {
    switch (mediaType) {
        case 'video': return 'fa-solid fa-video';
        case 'audio': return 'fa-solid fa-music';
        default: return 'fa-solid fa-image';
    }
}
function formatDate(dateStr) {
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}
function renderGallery() {
    const container = document.getElementById('gallery-container');
    if (!container)
        return;
    const eventList = Array.isArray(events) ? events : [];
    if (eventList.length === 0) {
        container.innerHTML = `
      <div class="col-12 text-center text-muted py-5">
        <i class="fa-solid fa-images fa-3x mb-3"></i>
        <p>No media found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    const filtered = currentMonth > 0
        ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
        : eventList;
    const mediaEvents = filtered.filter(e => e.media_url);
    if (mediaEvents.length === 0) {
        container.innerHTML = `
      <div class="col-12 text-center text-muted py-5">
        <i class="fa-solid fa-images fa-3x mb-3"></i>
        <p>No media found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    container.innerHTML = mediaEvents.slice(0, galleryLimit).map(e => {
        return `
      <div class="col-md-4 col-lg-3">
        <div class="gallery-item" onclick="showMedia(${e.id})">
          ${e.media_type === 'video'
            ? '<video src="' + e.media_url + '" muted></video>'
            : e.media_type === 'audio'
                ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
                : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'}
          <div class="gallery-overlay">
            <i class="${getMediaIcon(e.media_type)}"></i>
          </div>
        </div>
        <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
      </div>
    `;
    }).join('');
    const loadMoreBtn = document.getElementById('load-more-btn');
    if (loadMoreBtn) {
        loadMoreBtn.style.display = mediaEvents.length > galleryLimit ? '' : 'none';
    }
}
function renderMap() {
    const eventList = Array.isArray(events) ? events : [];
    const filtered = currentMonth > 0
        ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
        : eventList;
    const geoEvents = filtered.filter(e => e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));
    const placeholder = document.getElementById('map-placeholder');
    if (placeholder)
        placeholder.style.display = geoEvents.length ? 'none' : 'block';
    if (!geoEvents.length) {
        if (map)
            map.remove();
        map = null;
        markers = [];
        return;
    }
    if (!map) {
        map = L.map('map-container').setView([20, 0], 2);
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
            maxZoom: 18
        }).addTo(map);
    }
    markers.forEach(m => map.removeLayer(m));
    markers = [];
    const bounds = [];
    const markerIcon = L.divIcon({
        className: 'custom-marker',
        html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:24px;"></i>',
        iconSize: [24, 24],
        iconAnchor: [12, 24],
        popupAnchor: [0, -24]
    });
    geoEvents.forEach(e => {
        const m = L.marker([e.latitude, e.longitude], { icon: markerIcon }).addTo(map);
        let weatherHtml = '';
        if (e.weather_data) {
            try {
                const w = JSON.parse(e.weather_data);
                weatherHtml = `<br><small><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</small>`;
            }
            catch (_) { }
        }
        m.bindPopup(`
      <div class="map-popup">
        <h6>${escapeHtml(e.title)}</h6>
        <p>${formatDate(e.date)} — ${escapeHtml(e.location)}${weatherHtml}</p>
      </div>
    `);
        markers.push(m);
        bounds.push([e.latitude, e.longitude]);
    });
    if (bounds.length > 0) {
        map.fitBounds(bounds, { padding: [30, 30], maxZoom: 14 });
    }
    setTimeout(() => map.invalidateSize(), 100);
}
function showMedia(id) {
    const event = Array.isArray(events) ? events.find(e => e.id === id) : null;
    if (!event)
        return;
    const titleEl = document.getElementById('mediaModalTitle');
    const descEl = document.getElementById('mediaModalDesc');
    const bodyEl = document.getElementById('mediaModalBody');
    if (titleEl)
        titleEl.textContent = event.title;
    if (descEl) {
        descEl.innerHTML = `
      ${event.description ? renderMarkdown(event.description) : ''}
      <div class="mt-2 small opacity-75">
        <i class="fa-solid fa-calendar me-1"></i>${event.date}
        ${event.location ? '<i class="fa-solid fa-location-dot ms-2 me-1"></i>' + event.location : ''}
      </div>
    `;
    }
    let mediaHtml = '';
    if (event.media_url) {
        if (event.media_type === 'video') {
            mediaHtml = '<video controls class="w-100" src="' + event.media_url + '"></video>';
        }
        else if (event.media_type === 'audio') {
            mediaHtml = '<audio controls class="w-100" src="' + event.media_url + '"></audio>';
        }
        else {
            mediaHtml = '<img src="' + event.media_url + '" class="img-fluid" alt="' + event.title + '">';
        }
    }
    if (bodyEl)
        bodyEl.innerHTML = mediaHtml || '<p>No media available</p>';
    new window.bootstrap.Modal(document.getElementById('mediaModal')).show();
}
function escapeHtml(text) {
    if (!text)
        return '';
    return text.replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}
async function loadMoreGallery() {
    galleryPage++;
    const skip = (galleryPage - 1) * galleryLimit;
    try {
        const url = '/api/events?year=' + currentYear + '&limit=' + galleryLimit + '&skip=' + skip;
        const res = await fetch(url);
        let moreEvents;
        if (res.status === 401) {
            const pubRes = await fetch('/api/public?year=' + currentYear);
            moreEvents = pubRes.ok ? await pubRes.json() : [];
        } else if (!res.ok) {
            moreEvents = [];
        }
        else {
            moreEvents = await res.json();
        }
        if (!Array.isArray(moreEvents))
            moreEvents = [];
        const container = document.getElementById('gallery-container');
        const mediaEvents = moreEvents.filter(e => e.media_url);
        if (mediaEvents.length === 0) {
            const btn = document.getElementById('load-more-btn');
            if (btn)
                btn.style.display = 'none';
            return;
        }
        if (container) {
            container.innerHTML += mediaEvents.map(e => `
        <div class="col-md-4 col-lg-3">
          <div class="gallery-item" onclick="showMedia(${e.id})">
            ${e.media_type === 'video'
                ? '<video src="' + e.media_url + '" muted></video>'
                : e.media_type === 'audio'
                    ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
                    : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'}
            <div class="gallery-overlay">
              <i class="${getMediaIcon(e.media_type)}"></i>
            </div>
          </div>
          <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
        </div>
      `).join('');
        }
        events = [...events, ...moreEvents];
    }
    catch (err) {
        console.error('Failed to load more gallery:', err);
    }
}
async function getYearStats(year) {
    try {
        const res = await fetch('/api/events?year=' + year);
        let yearEvents;
        if (res.status === 401) {
            const pubRes = await fetch('/api/public?year=' + year);
            yearEvents = pubRes.ok ? await pubRes.json() : [];
        } else {
            yearEvents = res.ok ? await res.json() : [];
        }
        return {
            total: yearEvents.length,
            images: yearEvents.filter((e) => e.media_type === 'image').length,
            videos: yearEvents.filter((e) => e.media_type === 'video').length,
            audio: yearEvents.filter((e) => e.media_type === 'audio').length,
            locations: new Set(yearEvents.map((e) => e.location).filter(l => l)).size
        };
    }
    catch (err) {
        return { total: 0, images: 0, videos: 0, audio: 0, locations: 0 };
    }
}
async function updateCompare() {
    const year1El = document.getElementById('compare-year-1');
    const year2El = document.getElementById('compare-year-2');
    if (!year1El || !year2El)
        return;
    const year1 = year1El.value;
    const year2 = year2El.value;
    const [stats1, stats2] = await Promise.all([getYearStats(year1), getYearStats(year2)]);
    const stats1El = document.getElementById('compare-stats-1');
    const stats2El = document.getElementById('compare-stats-2');
    const resultEl = document.getElementById('compare-result');
    if (stats1El)
        stats1El.innerHTML = renderCompareStats(stats1);
    if (stats2El)
        stats2El.innerHTML = renderCompareStats(stats2);
    const diff = stats2.total - stats1.total;
    const trendColor = diff > 0 ? 'text-success' : diff < 0 ? 'text-danger' : 'text-muted';
    const trendIcon = diff > 0 ? 'fa-arrow-trend-up' : diff < 0 ? 'fa-arrow-trend-down' : 'fa-minus';
    if (resultEl) {
        resultEl.innerHTML = `
      <div class="text-center">
        <div class="mb-2"><i class="fa-solid ${trendIcon} fa-3x ${trendColor}"></i></div>
        <div class="h4 ${trendColor}">${diff > 0 ? '+' : ''}${diff}</div>
        <div class="small text-muted">events difference</div>
        <div class="mt-3">
          <strong>${year1}</strong>: ${stats1.total} events<br>
          <strong>${year2}</strong>: ${stats2.total} events
        </div>
      </div>
    `;
    }
}
function renderCompareStats(stats) {
    return `
    <div class="small">
      <div><strong>Total:</strong> ${stats.total} events</div>
      <div><i class="fa-solid fa-image me-1"></i>${stats.images}</div>
      <div><i class="fa-solid fa-video me-1"></i>${stats.videos}</div>
      <div><i class="fa-solid fa-music me-1"></i>${stats.audio}</div>
      <div><i class="fa-solid fa-location-dot me-1"></i>${stats.locations} locations</div>
    </div>
  `;
}
// Calendar
let calendarYear = new Date().getFullYear();
let calendarMonth = new Date().getMonth() + 1;
let calendarEventList = [];
function renderCalendar() {
    const filtered = Array.isArray(events) ? events : [];
    calendarEventList = filtered;
}
function renderCalendarView() {
    const grid = document.getElementById('calendar-grid');
    const title = document.getElementById('calendar-title');
    if (!grid || !title)
        return;
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
function calendarPrevMonth() {
    calendarMonth--;
    if (calendarMonth < 1) {
        calendarMonth = 12;
        calendarYear--;
    }
    renderCalendarView();
}
function calendarNextMonth() {
    calendarMonth++;
    if (calendarMonth > 12) {
        calendarMonth = 1;
        calendarYear++;
    }
    renderCalendarView();
}
function calendarToday() {
    calendarYear = new Date().getFullYear();
    calendarMonth = new Date().getMonth() + 1;
    renderCalendarView();
}
function showCalendarDay(dateStr) {
    const dayEvents = calendarEventList.filter(e => e.date === dateStr);
    const section = document.getElementById('calendar-selected-day');
    const dateEl = document.getElementById('calendar-selected-date');
    const listEl = document.getElementById('calendar-event-list');
    if (!section || !dateEl || !listEl)
        return;
    const d = new Date(dateStr + 'T12:00:00');
    dateEl.textContent = d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
    if (dayEvents.length === 0) {
        listEl.innerHTML = '<p class="text-muted text-center py-3">No events on this day</p>';
    }
    else {
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
// Memories
async function loadMemories() {
    try {
        const res = await fetch('/api/memories');
        if (res.status === 401) {
            const section = document.getElementById('memories-section');
            if (section) section.style.display = 'none';
            return;
        }
        const memories = await res.json();
        const section = document.getElementById('memories-section');
        if (!section)
            return;
        if (!memories || !memories.length) {
            section.style.display = 'none';
            return;
        }
        memories.sort((a, b) => a.years_ago - b.years_ago);
        section.style.display = 'block';
        section.innerHTML = '<h5 class="mb-3"><i class="fa-solid fa-clock-rotate-left me-2 text-primary"></i>On This Day</h5>'
            + memories.map((m) => '<div class="memories-item"><div class="fw-bold">' + escapeHtml(m.title) + '</div><div class="small text-muted">' + m.years_ago + ' year' + (m.years_ago > 1 ? 's' : '') + ' ago &middot; ' + m.date + '</div></div>').join('');
    }
    catch (_) { }
}
function initApp() {
    initTheme();
    const params = new URLSearchParams(window.location.search);
    const q = params.get('q');
    if (q) {
        const input = document.getElementById('search-input');
        if (input)
            input.value = q;
    }
    loadData();
    loadMemories();
    updateCompare();
    renderCalendarView();
    const compareYear1 = document.getElementById('compare-year-1');
    const compareYear2 = document.getElementById('compare-year-2');
    if (compareYear1 && compareYear2) {
        const y = new Date().getFullYear();
        for (let i = y + 1; i >= y - 10; i--) {
            const o1 = document.createElement('option');
            o1.value = String(i);
            o1.textContent = String(i);
            if (i === y)
                o1.selected = true;
            compareYear1.appendChild(o1);
            const o2 = document.createElement('option');
            o2.value = String(i);
            o2.textContent = String(i);
            if (i === y - 1)
                o2.selected = true;
            compareYear2.appendChild(o2);
        }
    }
    fetch('/api/version').then(r => r.json()).then(d => {
        const versionEl = document.getElementById('version-display');
        if (versionEl)
            versionEl.textContent = 'v' + d.version;
    }).catch(() => {
        const versionEl = document.getElementById('version-display');
        if (versionEl)
            versionEl.textContent = 'v1.0.0';
    });
}
document.addEventListener('DOMContentLoaded', initApp);
window.changeYear = changeYear;
window.searchEvents = searchEvents;
window.filterMonth = filterMonth;
window.showMedia = showMedia;
window.loadMoreGallery = loadMoreGallery;
window.calendarPrevMonth = calendarPrevMonth;
window.calendarNextMonth = calendarNextMonth;
window.calendarToday = calendarToday;
window.showCalendarDay = showCalendarDay;
