import 'json_utils.dart';

/// A geo-tagged event as returned inside the `GET /api/map` FeatureCollection.
///
/// The server emits a GeoJSON-like envelope but with flat fields rather than
/// `geometry`/`properties`, and `latitude`/`longitude` are plain numbers.
class MapFeature {
  const MapFeature({
    required this.id,
    required this.title,
    this.description = '',
    this.date = '',
    this.location = '',
    this.mediaType = '',
    this.mediaUrl = '',
    this.thumbnail = '',
    required this.latitude,
    required this.longitude,
  });

  final int id;
  final String title;
  final String description;
  final String date;
  final String location;
  final String mediaType;
  final String mediaUrl;
  final String thumbnail;
  final double latitude;
  final double longitude;

  factory MapFeature.fromJson(Map<String, dynamic> json) => MapFeature(
        id: asInt(json['id']),
        title: asString(json['title']),
        description: asString(json['description']),
        date: asString(json['date']),
        location: asString(json['location']),
        mediaType: asString(json['media_type']),
        mediaUrl: asString(json['media_url']),
        thumbnail: asString(json['thumbnail']),
        latitude: asDoubleOrNull(json['latitude']) ?? 0,
        longitude: asDoubleOrNull(json['longitude']) ?? 0,
      );

  String get previewUrl => thumbnail.isNotEmpty ? thumbnail : mediaUrl;
}

/// Envelope returned by `GET /api/map?year=`.
class MapData {
  const MapData({this.features = const []});

  final List<MapFeature> features;

  int get count => features.length;

  factory MapData.fromJson(Map<String, dynamic> json) => MapData(
        features: asMapList(json['features'])
            .map(MapFeature.fromJson)
            .toList(growable: false),
      );
}
