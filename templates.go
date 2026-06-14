package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"traces/internal/models"
)

var htmxTemplates *template.Template

func FormatDateTpl(dateStr string) string {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return date.Format("Jan 2, 2006")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func upper(s string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1])
}

func split(s, sep string) []string {
	return strings.Split(s, sep)
}

func renderTemplate(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := htmxTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("[HTMX] Template error %s: %v", name, err)
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

type EventRow struct {
	ID          int
	Title       string
	Date        string
	Location    string
	MediaType   string
	MediaURL    string
	Description string
	IsFavorite  bool
	PersonID    int
	PersonName  string
	PersonColor string
	Tags        string
	StartTime   string
	EndTime     string
	Recurring   string
	Latitude    float64
	Longitude   float64
}

type PersonRow struct {
	ID         int
	Name       string
	AvatarURL  string
	Bio        string
	BirthDate  string
	Color      string
	EventCount int
}

type TagRow struct {
	Name  string
	Count int
}

type CollectionRow struct {
	ID          int
	Name        string
	Description string
	Color       string
	EventCount  int
}

type UserRow struct {
	ID          int
	Username    string
	DisplayName string
	Color       string
	EventCount  int
}

type TemplateRow struct {
	ID         int
	Title      string
	Tags       string
	Location   string
	PersonName string
}

type TrashRow struct {
	ID        int
	Title     string
	Date      string
	DeletedAt string
}

func initTemplates() {
	funcMap := template.FuncMap{
		"escapeHtml":     models.EscapeHtml,
		"renderMarkdown": models.RenderMarkdown,
		"getMediaIcon":   models.GetMediaIcon,
		"formatDate":     FormatDateTpl,
		"formatDateTime": formatDateTime,
		"truncate":       truncate,
		"upper":          upper,
		"split":          split,
		"now":            time.Now,
	}
	var err error
	htmxTemplates, err = template.New("").Funcs(funcMap).Parse(htmxTemplateSource)
	if err != nil {
		log.Fatalf("[HTMX] Failed to parse templates: %v", err)
	}
}

func formatDateTime(dateStr string) string {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return date.Format("Jan 2, 2006")
}

func parseIntOrZero(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

const htmxTemplateSource = `
{{define "event-row"}}
<tr class="animate-in {{if .IsFavorite}}table-active{{end}}">
  <td class="ps-3 text-center"><input type="checkbox" class="event-select-cb" value="{{.ID}}" onchange="updateBatchButton()"></td>
  <td class="ps-1 fw-bold font-monospace" style="font-size:0.85rem">{{.Date}}</td>
  <td><span class="fw-medium">{{escapeHtml .Title}}</span></td>
  <td>{{if .Location}}<i class="fa-solid fa-location-dot me-1 text-muted" style="font-size:0.7rem"></i>{{escapeHtml .Location}}{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td>{{if .PersonName}}<span class="d-inline-flex align-items-center gap-1"><span class="color-dot" style="background:{{if .PersonColor}}{{.PersonColor}}{{else}}#7c3aed{{end}};width:8px;height:8px"></span>{{escapeHtml .PersonName}}</span>{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td>{{if .MediaURL}}<span class="media-type-badge {{.MediaType}}"><i class="fa-solid {{getMediaIcon .MediaType}} me-1"></i>{{.MediaType}}</span>{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td class="text-center"><i class="{{if .IsFavorite}}fa-solid{{else}}fa-regular{{end}} fa-star text-warning" style="cursor:pointer" onclick="toggleFav({{.ID}})" title="Toggle favorite"></i></td>
  <td class="text-end pe-3">
    <button class="btn btn-sm btn-outline-primary me-1" hx-get="/api/admin/events/{{.ID}}/edit" hx-target="#eventModalBody" hx-trigger="click" data-bs-toggle="modal" data-bs-target="#eventModal" title="Edit"><i class="fa-solid fa-pen"></i></button>
    <button class="btn btn-sm btn-outline-danger" hx-delete="/api/admin/events/{{.ID}}" hx-target="#event-list" hx-confirm="Delete this event?" title="Delete"><i class="fa-solid fa-trash"></i></button>
  </td>
</tr>
{{end}}

{{define "event-list"}}
{{range .}}{{template "event-row" .}}{{else}}
<tr><td colspan="8" class="text-center text-muted py-4">No events found</td></tr>
{{end}}
{{end}}

{{define "event-form"}}
<form hx-post="/api/admin/events" hx-target="#event-list" hx-swap="innerHTML">
  <input type="hidden" name="id" value="{{.ID}}">
  <div class="mb-3">
    <label class="form-label fw-bold">Title <span class="text-danger">*</span></label>
    <input type="text" class="form-control" name="title" value="{{escapeHtml .Title}}" required placeholder="Event name">
  </div>
  <div class="mb-3">
    <label class="form-label">Description</label>
    <textarea class="form-control" name="description" rows="3">{{escapeHtml .Description}}</textarea>
  </div>
  <div class="row g-3 mb-3">
    <div class="col-sm-4">
      <label class="form-label">Date</label>
      <input type="date" class="form-control" name="date" value="{{.Date}}">
    </div>
    <div class="col-sm-4">
      <label class="form-label">Start Time</label>
      <input type="time" class="form-control" name="start_time" value="{{.StartTime}}">
    </div>
    <div class="col-sm-4">
      <label class="form-label">End Time</label>
      <input type="time" class="form-control" name="end_time" value="{{.EndTime}}">
    </div>
  </div>
  <div class="row g-3 mb-3">
    <div class="col-sm-6">
      <label class="form-label">Location</label>
      <input type="text" class="form-control" name="location" value="{{escapeHtml .Location}}">
    </div>
    <div class="col-sm-6">
      <label class="form-label">Tags</label>
      <input type="text" class="form-control" name="tags" value="{{escapeHtml .Tags}}">
    </div>
  </div>
  <div class="row g-3 mb-3">
    <div class="col-sm-6">
      <label class="form-label">Recurring</label>
      <select class="form-select" name="recurring">
        <option value="">None</option>
        <option value="daily" {{if eq .Recurring "daily"}}selected{{end}}>Daily</option>
        <option value="weekly" {{if eq .Recurring "weekly"}}selected{{end}}>Weekly</option>
        <option value="monthly" {{if eq .Recurring "monthly"}}selected{{end}}>Monthly</option>
        <option value="yearly" {{if eq .Recurring "yearly"}}selected{{end}}>Yearly</option>
      </select>
    </div>
    <div class="col-sm-6">
      <label class="form-label">Media Type</label>
      <select class="form-select" name="media_type">
        <option value="image" {{if eq .MediaType "image"}}selected{{end}}>Image</option>
        <option value="video" {{if eq .MediaType "video"}}selected{{end}}>Video</option>
        <option value="audio" {{if eq .MediaType "audio"}}selected{{end}}>Audio</option>
      </select>
    </div>
  </div>
  <div class="row g-3 mb-3">
    <div class="col-sm-6">
      <label class="form-label">Latitude</label>
      <input type="number" step="any" class="form-control" name="latitude" value="{{if .Latitude}}{{.Latitude}}{{end}}" placeholder="e.g. 40.7580">
    </div>
    <div class="col-sm-6">
      <label class="form-label">Longitude</label>
      <input type="number" step="any" class="form-control" name="longitude" value="{{if .Longitude}}{{.Longitude}}{{end}}" placeholder="e.g. -73.9855">
    </div>
  </div>
  <div class="mb-3">
    <label class="form-label">Person Name</label>
    <input type="text" class="form-control" name="person_name" value="{{escapeHtml .PersonName}}" placeholder="Person name">
  </div>
  <button type="submit" class="btn btn-primary w-100 mt-2">Save Event</button>
</form>
{{end}}

{{define "person-card"}}
<div class="col-md-6 col-lg-4">
  <div class="person-card" hx-get="/api/admin/persons/{{.ID}}/events" hx-target="#event-list" hx-trigger="click" hx-swap="innerHTML">
    {{if .AvatarURL}}<img src="{{.AvatarURL}}" class="person-avatar" alt="">{{else}}<div class="person-avatar-placeholder" style="background:{{if .Color}}{{.Color}}{{else}}#7c3aed{{end}}">{{upper .Name}}</div>{{end}}
    <div class="person-info">
      <div class="name">{{escapeHtml .Name}}</div>
      <div class="meta">{{if .Bio}}{{truncate .Bio 50}}{{end}}</div>
    </div>
    <div class="person-stats">
      <span class="count">{{.EventCount}}</span>
      <span class="label">events</span>
    </div>
  </div>
</div>
{{end}}

{{define "person-list"}}
{{range .}}{{template "person-card" .}}{{else}}
<div class="col-12"><div class="empty-state"><i class="fa-solid fa-users"></i><p>No people found</p></div></div>
{{end}}
{{end}}

{{define "tag-row"}}
<tr>
  <td><span class="badge bg-primary">{{escapeHtml .Name}}</span></td>
  <td>{{.Count}}</td>
  <td>
    <button class="btn btn-sm btn-outline-info me-1" hx-get="/api/admin/tags/{{.Name}}/rename" hx-target="#tag-manager-table" title="Rename"><i class="fa-solid fa-pencil"></i></button>
    <button class="btn btn-sm btn-outline-danger" hx-delete="/api/admin/tags/{{.Name}}" hx-target="#tag-manager-table" hx-confirm='Delete tag "{{escapeHtml .Name}}"? This will remove it from all events.' title="Delete"><i class="fa-solid fa-trash-can"></i></button>
  </td>
</tr>
{{end}}

{{define "tag-table"}}
<div class="table-responsive">
  <table class="table table-sm table-hover">
    <thead><tr><th>Tag</th><th>Count</th><th>Actions</th></tr></thead>
    <tbody>
      {{range .}}{{template "tag-row" .}}{{else}}
      <tr><td colspan="3" class="text-muted text-center py-3">No tags found</td></tr>
      {{end}}
    </tbody>
  </table>
</div>
{{end}}

{{define "tag-cloud-htmx"}}
{{range .}}<span class="badge bg-primary me-1 mb-1" style="cursor:pointer" hx-get="/api/admin/events/search?tag={{.Name}}" hx-target="#event-list" title="{{.Count}} events">{{escapeHtml .Name}} ({{.Count}})</span>{{else}}<div class="text-muted">No tags found</div>{{end}}
{{end}}

{{define "collection-form"}}
<form hx-post="/api/admin/collections" hx-target="#collection-list" hx-swap="innerHTML">
  <input type="hidden" name="id" value="{{.ID}}">
  <div class="mb-3">
    <label class="form-label">Name <span class="text-danger">*</span></label>
    <input type="text" class="form-control" name="name" value="{{escapeHtml .Name}}" required>
  </div>
  <div class="mb-3">
    <label class="form-label">Description</label>
    <textarea class="form-control" name="description" rows="2">{{escapeHtml .Description}}</textarea>
  </div>
  <div class="mb-3">
    <label class="form-label">Color</label>
    <input type="color" class="form-control form-control-color" name="color" value="{{.Color}}" style="width:60px;height:38px;padding:3px">
  </div>
  <button type="submit" class="btn btn-primary">Save Collection</button>
</form>
{{end}}

{{define "collection-card"}}
<div class="col-md-6 col-lg-4">
  <div class="card h-100" style="border-left:4px solid {{.Color}}">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-start mb-2">
        <div>
          <h6 class="fw-bold mb-1">{{escapeHtml .Name}}</h6>
          {{if .Description}}<p class="text-muted small mb-0">{{truncate .Description 80}}</p>{{end}}
        </div>
        <div class="d-flex gap-1">
          <button class="btn btn-sm btn-outline-primary" hx-get="/api/admin/collections/{{.ID}}/edit" hx-target="#collectionFormContainer" title="Edit"><i class="fa-solid fa-pen"></i></button>
          <button class="btn btn-sm btn-outline-danger" hx-delete="/api/admin/collections/{{.ID}}" hx-target="#collection-list" hx-confirm="Delete this collection?" title="Delete"><i class="fa-solid fa-trash"></i></button>
        </div>
      </div>
      <div class="d-flex justify-content-between align-items-center mt-2">
        <span class="badge bg-primary">{{.EventCount}} events</span>
      </div>
    </div>
  </div>
</div>
{{end}}

{{define "collection-list-htmx"}}
{{range .}}{{template "collection-card" .}}{{else}}
<div class="col-12"><div class="empty-state"><i class="fa-solid fa-folder-open"></i><p>No collections yet</p></div></div>
{{end}}
{{end}}

{{define "template-row"}}
<tr>
  <td><span class="fw-medium">{{escapeHtml .Title}}</span></td>
  <td>{{if .Tags}}<span class="text-muted">{{escapeHtml .Tags}}</span>{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td>{{if .Location}}{{escapeHtml .Location}}{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td>{{if .PersonName}}{{escapeHtml .PersonName}}{{else}}<span class="text-muted">&mdash;</span>{{end}}</td>
  <td class="text-end pe-3">
    <button class="btn btn-sm btn-outline-primary me-1" hx-get="/api/admin/templates/{{.ID}}/edit" hx-target="#templateFormContainer" title="Edit"><i class="fa-solid fa-pen"></i></button>
    <button class="btn btn-sm btn-outline-danger" hx-delete="/api/admin/templates/{{.ID}}" hx-target="#template-list" hx-confirm="Delete this template?" title="Delete"><i class="fa-solid fa-trash"></i></button>
  </td>
</tr>
{{end}}

{{define "template-list-htmx"}}
{{range .}}{{template "template-row" .}}{{else}}
<tr><td colspan="5" class="text-center text-muted py-4">No templates found</td></tr>
{{end}}
{{end}}

{{define "user-card"}}
<div class="col-md-6 col-lg-4">
  <div class="person-card">
    <div class="person-avatar-placeholder" style="background:{{.Color}}"><i class="fa-solid fa-user" style="color:white;font-size:1.2rem"></i></div>
    <div class="person-info">
      <div class="name">{{escapeHtml .DisplayName}}</div>
      <div class="meta">@{{escapeHtml .Username}}{{if .EventCount}} &middot; {{.EventCount}} events{{end}}</div>
    </div>
    <div class="person-stats">
      <button class="btn btn-sm btn-outline-primary" hx-get="/api/admin/users/{{.ID}}/edit" hx-target="#userFormContainer" title="Edit"><i class="fa-solid fa-pen"></i></button>
      <button class="btn btn-sm btn-outline-danger ms-1" hx-delete="/api/admin/users/{{.ID}}" hx-target="#user-list" hx-confirm="Delete this user?"><i class="fa-solid fa-trash"></i></button>
    </div>
  </div>
</div>
{{end}}

{{define "user-list-htmx"}}
{{range .}}{{template "user-card" .}}{{else}}
<div class="col-12"><div class="empty-state"><i class="fa-solid fa-users"></i><p>No users yet</p></div></div>
{{end}}
{{end}}

{{define "trash-row"}}
<tr>
  <td class="fw-medium">{{escapeHtml .Title}}</td>
  <td>{{.Date}}</td>
  <td class="text-muted small">{{.DeletedAt}}</td>
  <td>
    <button class="btn btn-sm btn-outline-success me-1" hx-post="/api/admin/trash/{{.ID}}/restore" hx-target="#trash-list"><i class="fa-solid fa-rotate-left"></i> Restore</button>
    <button class="btn btn-sm btn-outline-danger" hx-delete="/api/admin/trash/{{.ID}}" hx-target="#trash-list" hx-confirm="Permanently delete this event?"><i class="fa-solid fa-xmark"></i> Delete</button>
  </td>
</tr>
{{end}}

{{define "trash-list-htmx"}}
{{if .}}
<div class="table-responsive">
  <table class="table table-sm">
    <thead><tr><th>Title</th><th>Date</th><th>Deleted</th><th>Actions</th></tr></thead>
    <tbody>
      {{range .}}{{template "trash-row" .}}{{end}}
    </tbody>
  </table>
</div>
{{else}}
<div class="text-muted py-4 text-center"><i class="fa-solid fa-trash-can me-2"></i>Trash is empty</div>
{{end}}
{{end}}

{{define "timeline-item"}}
<div class="timeline-item {{if .IsRight}}right{{else}}left{{end}}" id="event-{{.ID}}">
  <div class="timeline-content" onclick="{{if .MediaURL}}showMedia({{.ID}}){{end}}">
    <div class="d-flex justify-content-between align-items-start">
      <div class="timeline-date">{{formatDate .Date}}{{if .StartTime}} <i class="fa-regular fa-clock ms-1"></i>{{.StartTime}}{{end}}{{if .EndTime}}&ndash;{{.EndTime}}{{end}}{{if .Recurring}} <span class="badge bg-info ms-1"><i class="fa-solid fa-rotate"></i> {{.Recurring}}</span>{{end}}</div>
      <i class="{{if .IsFavorite}}fa-solid{{else}}fa-regular{{end}} fa-star text-warning" style="cursor:pointer;font-size:0.85rem" onclick="event.stopPropagation();toggleFav({{.ID}})" title="{{if .IsFavorite}}Unfavorite{{else}}Favorite{{end}}"></i>
    </div>
    <div class="timeline-title">{{escapeHtml .Title}}</div>
    <div class="timeline-location"><i class="fa-solid fa-location-dot me-1"></i>{{escapeHtml .Location}}</div>
    {{if .Tags}}<div class="timeline-people mb-2"><i class="fa-solid fa-tags me-1"></i>{{range (split .Tags ",")}}<span class="badge bg-secondary me-1">{{escapeHtml .}}</span>{{end}}</div>{{end}}
    {{if .Description}}<div class="timeline-desc md-content">{{renderMarkdown .Description}}</div>{{end}}
  </div>
</div>
{{end}}

{{define "template-form"}}
<form hx-post="/api/admin/templates" hx-target="#template-list" hx-swap="innerHTML">
  <input type="hidden" name="id" value="{{.ID}}">
  <div class="mb-3">
    <label class="form-label">Title <span class="text-danger">*</span></label>
    <input type="text" class="form-control" name="title" value="{{escapeHtml .Title}}" required>
  </div>
  <div class="mb-3">
    <label class="form-label">Tags</label>
    <input type="text" class="form-control" name="tags" value="{{escapeHtml .Tags}}">
  </div>
  <div class="mb-3">
    <label class="form-label">Location</label>
    <input type="text" class="form-control" name="location" value="{{escapeHtml .Location}}">
  </div>
  <button type="submit" class="btn btn-primary">Save Template</button>
</form>
{{end}}

{{define "user-form"}}
<form hx-post="/api/admin/users" hx-target="#user-list" hx-swap="innerHTML">
  <input type="hidden" name="id" value="{{.ID}}">
  <div class="mb-3">
    <label class="form-label">Username</label>
    <input type="text" class="form-control" name="username" value="{{escapeHtml .Username}}" required>
  </div>
  <div class="mb-3">
    <label class="form-label">Display Name</label>
    <input type="text" class="form-control" name="display_name" value="{{escapeHtml .DisplayName}}">
  </div>
  <div class="mb-3">
    <label class="form-label">Color</label>
    <input type="color" class="form-control form-control-color" name="color" value="{{.Color}}" style="width:60px;height:40px">
  </div>
  <button type="submit" class="btn btn-primary w-100">Save User</button>
</form>
{{end}}

{{define "htmx-utils"}}
<script>
// Close modal on successful htmx request
document.addEventListener('htmx:afterRequest', function(evt) {
  if (evt.detail.successful) {
    var modal = evt.detail.target.closest('.modal');
    if (modal) {
      var bsModal = bootstrap.Modal.getInstance(modal);
      if (bsModal) bsModal.hide();
    }
  }
});
// Auto-load CSRF token before htmx requests
document.addEventListener('htmx:configRequest', function(evt) {
  var csrfToken = window.__csrfToken || '';
  if (csrfToken && (evt.detail.verb === 'post' || evt.detail.verb === 'delete' || evt.detail.verb === 'put')) {
    evt.detail.headers['X-CSRF-Token'] = csrfToken;
  }
});
// Fetch CSRF token on page load
document.addEventListener('DOMContentLoaded', function() {
  fetch('/api/csrf-token').then(r => r.json()).then(d => { window.__csrfToken = d.token; }).catch(function(){});
});
</script>
{{end}}
`
