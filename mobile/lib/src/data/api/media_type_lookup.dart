import 'package:http_parser/http_parser.dart';

/// Returns the file name component of [path] (tolerating Windows separators).
String fileName(String path) {
  final normalized = path.replaceAll('\\', '/');
  final slash = normalized.lastIndexOf('/');
  return slash < 0 ? normalized : normalized.substring(slash + 1);
}

/// Lower-cased extension (without the dot) of [path], or `''`.
String extensionOf(String path) {
  final name = fileName(path);
  final dot = name.lastIndexOf('.');
  return dot < 0 ? '' : name.substring(dot + 1).toLowerCase();
}

/// Maps a file extension to the content type the TRACES upload handler
/// validates against (`image/`, `video/`, `audio/` prefixes).
///
/// Returns `null` for unknown extensions so Dio falls back to
/// `application/octet-stream`.
MediaType? guessContentType(String filePath) {
  switch (extensionOf(filePath)) {
    case 'jpg':
    case 'jpeg':
      return MediaType('image', 'jpeg');
    case 'png':
      return MediaType('image', 'png');
    case 'gif':
      return MediaType('image', 'gif');
    case 'webp':
      return MediaType('image', 'webp');
    case 'avif':
      return MediaType('image', 'avif');
    case 'bmp':
      return MediaType('image', 'bmp');
    case 'tif':
    case 'tiff':
      return MediaType('image', 'tiff');
    case 'svg':
      return MediaType('image', 'svg+xml');
    case 'mp4':
      return MediaType('video', 'mp4');
    case 'webm':
      return MediaType('video', 'webm');
    case 'mov':
      return MediaType('video', 'quicktime');
    case 'avi':
      return MediaType('video', 'x-msvideo');
    case 'mkv':
      return MediaType('video', 'x-matroska');
    case '3gp':
      return MediaType('video', '3gpp');
    case 'ogv':
      return MediaType('video', 'ogg');
    case 'mp3':
      return MediaType('audio', 'mpeg');
    case 'wav':
      return MediaType('audio', 'wav');
    case 'ogg':
    case 'oga':
      return MediaType('audio', 'ogg');
    case 'flac':
      return MediaType('audio', 'flac');
    case 'aac':
      return MediaType('audio', 'aac');
    case 'm4a':
      return MediaType('audio', 'mp4');
    case 'wma':
      return MediaType('audio', 'x-ms-wma');
    case 'opus':
      return MediaType('audio', 'opus');
    default:
      return null;
  }
}
