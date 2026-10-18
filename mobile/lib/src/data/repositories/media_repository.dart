import '../api/api_client.dart';
import '../models/json_utils.dart';
import '../models/upload_result.dart';

/// Multipart media uploads (`POST /api/upload`).
class MediaRepository {
  MediaRepository(this._client);

  final ApiClient _client;

  /// Uploads [filePath] as [mediaType] (`image`, `video`, or `audio`).
  ///
  /// The server picks the multipart field name from `media_type` and validates
  /// the content type against it.
  Future<UploadResult> upload({
    required String filePath,
    required String mediaType,
    void Function(int sent, int total)? onProgress,
  }) async {
    final data = await _client.uploadMedia(
      filePath: filePath,
      mediaType: mediaType,
      onProgress: onProgress,
    );
    return UploadResult.fromJson(asMap(data));
  }
}
