export {};
declare const L: any;

let mapInstance: any = null;
let clusterGroup: any = null;
let markerList: any[] = [];
let visibleEvents: any[] = [];
let mapEventsData: any[] = [];

function initMap(): void {
  mapInstance = L.map('map').setView([40.7128, -74.0060], 5);
  L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    maxZoom: 19
  }).addTo(mapInstance);
  const filterInput = document.getElementById('location-filter') as HTMLInputElement;
  if (filterInput) {
    filterInput.addEventListener('input', () => {
      applyLocationFilter(filterInput.value.trim());
    });
  }
  loadMapData();
}

function applyLocationFilter(query: string): void {
  if (!query) {
    visibleEvents = mapEventsData.slice();
  } else {
    visibleEvents = mapEventsData.filter((event: any) =>
      (event.location || '').toLowerCase().includes(query.toLowerCase())
    );
  }
  renderMarkers();
  renderEventList();
}

async function loadMapData(): Promise<void> {
  try {
    const res = await fetch('/api/map');
    const data = await res.json();
    mapEventsData = data.features || [];
    visibleEvents = mapEventsData.slice();
    renderMarkers();
    renderEventList();
  } catch (err) {
    console.error('Failed to load map data:', err);
    const list = document.getElementById('event-list');
    if (list) list.innerHTML = '<div class="empty-state"><i class="fa-solid fa-triangle-exclamation"></i><p>Failed to load map data</p></div>';
  }
}

function renderMarkers(): void {
  if (clusterGroup) clusterGroup.clearLayers();
  markerList = [];
  if (visibleEvents.length === 0) return;

  const mediaIcons: Record<string, any> = {
    image: L.divIcon({ className: 'map-marker-icon', html: '<div style="background:#7c3aed;color:white;width:28px;height:28px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:14px;box-shadow:0 2px 6px rgba(0,0,0,0.3)"><i class="fa-solid fa-image"></i></div>', iconSize: [28, 28], iconAnchor: [14, 14] }),
    video: L.divIcon({ className: 'map-marker-icon', html: '<div style="background:#ef4444;color:white;width:28px;height:28px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:14px;box-shadow:0 2px 6px rgba(0,0,0,0.3)"><i class="fa-solid fa-video"></i></div>', iconSize: [28, 28], iconAnchor: [14, 14] }),
    audio: L.divIcon({ className: 'map-marker-icon', html: '<div style="background:#f59e0b;color:white;width:28px;height:28px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:14px;box-shadow:0 2px 6px rgba(0,0,0,0.3)"><i class="fa-solid fa-music"></i></div>', iconSize: [28, 28], iconAnchor: [14, 14] }),
    default: L.divIcon({ className: 'map-marker-icon', html: '<div style="background:#6b7280;color:white;width:28px;height:28px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:14px;box-shadow:0 2px 6px rgba(0,0,0,0.3)"><i class="fa-solid fa-location-dot"></i></div>', iconSize: [28, 28], iconAnchor: [14, 14] })
  };

  const bounds: [number, number][] = [];
  const markers: any[] = [];
  visibleEvents.forEach((event: any) => {
    if (!event.latitude || !event.longitude) return;
    const icon = mediaIcons[event.media_type] || mediaIcons.default;
    const marker = L.marker([event.latitude, event.longitude], { icon: icon });
    let mediaHtml = '';
    if (event.media_url) {
      if (event.media_type === 'image') {
        mediaHtml = '<img src="' + (event.thumbnail || event.media_url) + '" style="max-width:200px;border-radius:4px;margin-top:8px">';
      } else if (event.media_type === 'video') {
        mediaHtml = event.thumbnail
          ? '<img src="' + event.thumbnail + '" style="max-width:200px;border-radius:4px;margin-top:8px">'
          : '<video controls style="max-width:200px;border-radius:4px;margin-top:8px" src="' + event.media_url + '"></video>';
      } else {
        mediaHtml = '<audio controls style="margin-top:8px" src="' + event.media_url + '"></audio>';
      }
    }
    marker.bindPopup('<div style="min-width:150px"><strong>' + event.title + '</strong><br><small style="color:#7c3aed">' + event.date + '</small><br><small>' + (event.location || '') + '</small>' + (event.description ? '<p style="margin-top:4px;font-size:0.85rem">' + event.description + '</p>' : '') + mediaHtml + '</div>');
    markers.push(marker);
    markerList.push(marker);
    bounds.push([event.latitude, event.longitude]);
  });

  if (markers.length === 0) return;
  clusterGroup = L.markerClusterGroup({ chunkedLoading: true, maxClusterRadius: 40 });
  clusterGroup.addLayers(markers);
  mapInstance.addLayer(clusterGroup);

  if (bounds.length > 0) mapInstance.fitBounds(bounds, { padding: [50, 50], maxZoom: 12 });
}

function renderEventList(): void {
  const list = document.getElementById('event-list');
  if (!list) return;
  if (visibleEvents.length === 0) {
    list.innerHTML = '<div class="empty-state"><i class="fa-solid fa-map-marker-alt"></i><p>No events with location data</p><small>Add latitude and longitude to events in the admin panel to see them here.</small></div>';
    return;
  }
  list.innerHTML = visibleEvents.map((event: any, index: number) =>
    '<div class="event-marker-item" onclick="focusEvent(' + index + ')"><div class="date"><i class="fa-solid fa-calendar me-1"></i>' + event.date + '</div><div class="title">' + event.title + '</div><div class="location"><i class="fa-solid fa-location-dot me-1"></i>' + (event.location || 'Unknown') + '</div>' + (event.media_type ? '<span class="badge bg-secondary" style="font-size:0.7rem">' + event.media_type + '</span>' : '') + '</div>'
  ).join('');
}

function focusEvent(index: number): void {
  const event = visibleEvents[index];
  const marker = markerList[index];
  if (!event || !marker) return;
  if (event.latitude && event.longitude) {
    mapInstance.setView([event.latitude, event.longitude], 14, { animate: true });
    if (clusterGroup && typeof clusterGroup.zoomToShowLayer === 'function') {
      clusterGroup.zoomToShowLayer(marker, () => marker.openPopup());
    } else {
      marker.openPopup();
    }
  }
}

initMap();

(window as any).focusEvent = focusEvent;
