/// Tolerant helpers for decoding loosely-typed JSON returned by the TRACES API.
///
/// The Go handlers marshal `null` for unset pointer fields and occasionally
/// numbers as strings, so every accessor here degrades gracefully instead of
/// throwing on an unexpected shape.
library;

String asString(dynamic value, {String fallback = ''}) {
  if (value == null) return fallback;
  if (value is String) return value;
  return value.toString();
}

int asInt(dynamic value, {int fallback = 0}) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value) ?? fallback;
  return fallback;
}

int? asIntOrNull(dynamic value) {
  if (value == null) return null;
  if (value is int) return value;
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value);
  return null;
}

double? asDoubleOrNull(dynamic value) {
  if (value == null) return null;
  if (value is double) return value;
  if (value is num) return value.toDouble();
  if (value is String) return double.tryParse(value);
  return null;
}

bool asBool(dynamic value, {bool fallback = false}) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  if (value is String) {
    final v = value.toLowerCase();
    if (v == 'true' || v == '1') return true;
    if (v == 'false' || v == '0') return false;
  }
  return fallback;
}

Map<String, dynamic> asMap(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) {
    return value.map((key, v) => MapEntry(key.toString(), v));
  }
  return const {};
}

List<dynamic> asList(dynamic value) {
  if (value is List) return value;
  return const [];
}

List<Map<String, dynamic>> asMapList(dynamic value) =>
    asList(value).map(asMap).toList(growable: false);

Map<String, int> asIntMap(dynamic value) {
  final out = <String, int>{};
  asMap(value).forEach((key, v) => out[key] = asInt(v));
  return out;
}
