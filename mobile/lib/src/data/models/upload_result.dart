import 'json_utils.dart';

/// Result of `POST /api/upload` after a successful multipart upload.
class UploadResult {
  const UploadResult({
    required this.url,
    required this.mediaType,
    this.thumbnail = '',
    this.latitude,
    this.longitude,
    this.locationSuggestion = '',
  });

  final String url;
  final String mediaType;
  final String thumbnail;
  final double? latitude;
  final double? longitude;
  final String locationSuggestion;

  bool get hasExifGeo => latitude != null && longitude != null;

  factory UploadResult.fromJson(Map<String, dynamic> json) => UploadResult(
        url: asString(json['url']),
        mediaType: asString(json['media_type']),
        thumbnail: asString(json['thumbnail']),
        latitude: asDoubleOrNull(json['latitude']),
        longitude: asDoubleOrNull(json['longitude']),
        locationSuggestion: asString(json['location_suggestion']),
      );
}
