import 'json_utils.dart';

/// Year-in-review summary used by the multi-year comparison view
/// (`GET /api/wrapped?year=`).
class WrappedSummary {
  const WrappedSummary({
    this.year = 0,
    this.totalEvents = 0,
    this.topEvent = '',
    this.topEventDate = '',
    this.longestStreak = 0,
    this.mostTagsTitle = '',
    this.mostTagsCount = 0,
    this.favoriteCount = 0,
    this.busiestMonth = '',
    this.busiestMonthCount = 0,
    this.totalMedia = 0,
    this.byMonth = const {},
  });

  final int year;
  final int totalEvents;
  final String topEvent;
  final String topEventDate;
  final int longestStreak;
  final String mostTagsTitle;
  final int mostTagsCount;
  final int favoriteCount;
  final String busiestMonth;
  final int busiestMonthCount;
  final int totalMedia;
  final Map<String, int> byMonth;

  factory WrappedSummary.fromJson(Map<String, dynamic> json) => WrappedSummary(
        year: asInt(json['year']),
        totalEvents: asInt(json['total_events']),
        topEvent: asString(json['top_event']),
        topEventDate: asString(json['top_event_date']),
        longestStreak: asInt(json['longest_streak']),
        mostTagsTitle: asString(json['most_tags_title']),
        mostTagsCount: asInt(json['most_tags_count']),
        favoriteCount: asInt(json['favorite_count']),
        busiestMonth: asString(json['busiest_month']),
        busiestMonthCount: asInt(json['busiest_month_count']),
        totalMedia: asInt(json['total_media']),
        byMonth: asIntMap(json['by_month']),
      );
}
