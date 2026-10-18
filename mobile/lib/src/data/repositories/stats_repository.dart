import '../api/api_client.dart';
import '../models/event_stats.dart';
import '../models/json_utils.dart';
import '../models/map_data.dart';
import '../models/tag_count.dart';
import '../models/wrapped_summary.dart';

/// Map, heatmap, tags, and statistics reads.
class StatsRepository {
  StatsRepository(this._client);

  final ApiClient _client;

  /// `{ "YYYY-MM-DD": count }` for the 365-day activity heatmap.
  Future<Map<String, int>> fetchContributions(int year) async {
    final data = await _client.getJson(
      '/api/contributions',
      query: {'year': '$year'},
    );
    return asIntMap(data);
  }

  Future<MapData> fetchMapData(int year) async {
    final data = await _client.getJson('/api/map', query: {'year': '$year'});
    return MapData.fromJson(asMap(data));
  }

  Future<List<TagCount>> fetchTags(int year) async {
    final data = await _client.getJson('/api/tags', query: {'year': '$year'});
    return asMapList(data).map(TagCount.fromJson).toList(growable: false);
  }

  Future<EventStats> fetchStats(int year) async {
    final data = await _client.getJson('/api/stats', query: {'year': '$year'});
    return EventStats.fromJson(asMap(data));
  }

  Future<WrappedSummary> fetchWrapped(int year) async {
    final data = await _client.getJson('/api/wrapped', query: {'year': '$year'});
    return WrappedSummary.fromJson(asMap(data));
  }
}
