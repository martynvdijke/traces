import 'json_utils.dart';

/// A tag together with how many events carry it (`GET /api/tags`).
class TagCount {
  const TagCount({required this.name, required this.count});

  final String name;
  final int count;

  factory TagCount.fromJson(Map<String, dynamic> json) => TagCount(
        name: asString(json['name']),
        count: asInt(json['count']),
      );
}
