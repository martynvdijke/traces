import '../api/api_client.dart';
import '../models/event_filter.dart';
import '../models/json_utils.dart';
import '../models/timeline_event.dart';

/// Timeline read/write operations against `/api/events`
/// and `/api/events/search`.
class EventsRepository {
  EventsRepository(this._client);

  final ApiClient _client;

  static const String listPath = '/api/events';
  static const String searchPath = '/api/events/search';

  Future<List<TimelineEvent>> fetchTimeline(EventFilter filter) async {
    // `/api/events` lacks person/media/query filters, so route those through
    // the search endpoint.
    final path = filter.requiresSearchEndpoint ? searchPath : listPath;
    final data = await _client.getJson(path, query: filter.toQueryParameters());
    var events = asMapList(data).map(TimelineEvent.fromJson).toList();
    if (filter.favoritesOnly) {
      events = events.where((event) => event.isFavorite).toList();
    }
    return events;
  }

  Future<TimelineEvent> saveEvent(TimelineEvent event) async {
    final data = await _client.postJson(listPath, body: event.toJson());
    return TimelineEvent.fromJson(asMap(data));
  }

  Future<void> deleteEvent(int id) async {
    await _client.deleteJson(listPath, query: {'id': '$id'});
  }

  Future<void> toggleFavorite(int id) async {
    await _client.postJson('/api/events/favorite', body: {'id': id});
  }
}
