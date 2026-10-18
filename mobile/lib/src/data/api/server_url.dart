/// Normalizes a user-entered TRACES server address into a base URL.
///
/// - trims whitespace
/// - prepends `http://` when no scheme is given (self-hosted servers are
///   commonly plain HTTP on a LAN)
/// - strips trailing slashes
String normalizeServerUrl(String input) {
  var value = input.trim();
  if (value.isEmpty) return '';
  if (!value.contains('://')) {
    value = 'http://$value';
  }
  while (value.endsWith('/')) {
    value = value.substring(0, value.length - 1);
  }
  return value;
}

/// Whether [input] looks like a usable HTTP(S) server address.
bool isValidServerUrl(String input) {
  final normalized = normalizeServerUrl(input);
  if (normalized.isEmpty) return false;
  final uri = Uri.tryParse(normalized);
  if (uri == null) return false;
  if (uri.scheme != 'http' && uri.scheme != 'https') return false;
  return uri.host.isNotEmpty;
}

/// Resolves a possibly-relative media path (`/media/ab/x.jpg`) against [baseUrl].
String absoluteUrl(String baseUrl, String path) {
  if (path.isEmpty || baseUrl.isEmpty) return path;
  if (path.startsWith('http://') || path.startsWith('https://')) return path;
  final base = baseUrl.endsWith('/')
      ? baseUrl.substring(0, baseUrl.length - 1)
      : baseUrl;
  final suffix = path.startsWith('/') ? path : '/$path';
  return '$base$suffix';
}
