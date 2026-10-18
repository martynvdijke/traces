import 'json_utils.dart';

/// Aggregate statistics for a year, mirroring `models.EventStats`
/// (`GET /api/stats?year=`).
class EventStats {
  const EventStats({
    this.total = 0,
    this.byMonth = const {},
    this.byTag = const {},
    this.byMedia = const {},
    this.byYear = const {},
    this.yearOverYear = const {},
    this.mediaTotal = 0,
    this.withLocation = 0,
    this.withMedia = 0,
    this.withGeo = 0,
    this.personCount = 0,
    this.totalYears = 0,
  });

  final int total;
  final Map<String, int> byMonth;
  final Map<String, int> byTag;
  final Map<String, int> byMedia;
  final Map<String, int> byYear;
  final Map<String, int> yearOverYear;
  final int mediaTotal;
  final int withLocation;
  final int withMedia;
  final int withGeo;
  final int personCount;
  final int totalYears;

  factory EventStats.fromJson(Map<String, dynamic> json) => EventStats(
        total: asInt(json['total']),
        byMonth: asIntMap(json['by_month']),
        byTag: asIntMap(json['by_tag']),
        byMedia: asIntMap(json['by_media']),
        byYear: asIntMap(json['by_year']),
        yearOverYear: asIntMap(json['year_over_year']),
        mediaTotal: asInt(json['media_total']),
        withLocation: asInt(json['with_location']),
        withMedia: asInt(json['with_media']),
        withGeo: asInt(json['with_geo']),
        personCount: asInt(json['person_count']),
        totalYears: asInt(json['total_years']),
      );
}
