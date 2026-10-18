import 'package:dio/dio.dart';
import 'package:http_parser/http_parser.dart';

import 'api_exception.dart';
import 'media_type_lookup.dart';

/// Extracts the value of cookie [name] from raw `Set-Cookie` headers.
///
/// Returns `null` when the cookie is absent or was cleared (empty value), which
/// lets callers treat logout / expiry uniformly.
String? parseSetCookie(List<String>? headers, String name) {
  if (headers == null) return null;
  for (final header in headers) {
    final pair = header.split(';').first.trim();
    final equals = pair.indexOf('=');
    if (equals <= 0) continue;
    final key = pair.substring(0, equals).trim();
    if (key != name) continue;
    final value = pair.substring(equals + 1).trim();
    return value.isEmpty ? null : value;
  }
  return null;
}

/// The session cookie value from a login/OIDC response.
String? parseSessionCookie(List<String>? headers) =>
    parseSetCookie(headers, 'session');

/// Extracts cookie [name] from a raw `Cookie:` request header value
/// (`a=b; session=xyz`). Used to read the session captured by the OIDC WebView.
String? parseCookieHeader(String? header, String name) {
  if (header == null || header.isEmpty) return null;
  for (final part in header.split(';')) {
    final pair = part.trim();
    final equals = pair.indexOf('=');
    if (equals <= 0) continue;
    if (pair.substring(0, equals).trim() != name) continue;
    final value = pair.substring(equals + 1).trim();
    return value.isEmpty ? null : value;
  }
  return null;
}

/// Outcome of a successful `POST /api/login`.
class LoginOutcome {
  const LoginOutcome({this.sessionCookie});

  final String? sessionCookie;
}

/// Thin, stateless-ish wrapper around [Dio] for the TRACES `/api` surface.
///
/// Responsibilities:
/// - attach the `session` cookie (the server auth is cookie-based and the
///   login response carries no token in its JSON body)
/// - attach `X-CSRF-Token` to every mutating request
/// - translate [DioException] into [ApiException]
///
/// Cookies are managed manually rather than via a cookie jar so the session can
/// be persisted in secure storage and shared with the OIDC WebView flow.
class ApiClient {
  ApiClient({String baseUrl = '', Dio? dio}) : _dio = dio ?? Dio() {
    _dio.options
      ..baseUrl = baseUrl
      ..connectTimeout = const Duration(seconds: 15)
      ..receiveTimeout = const Duration(minutes: 2)
      ..sendTimeout = const Duration(minutes: 5)
      ..responseType = ResponseType.json
      ..headers['Accept'] = 'application/json';
    _dio.interceptors.add(
      InterceptorsWrapper(onRequest: _onRequest),
    );
  }

  final Dio _dio;
  String? _sessionCookie;
  String? _csrfToken;

  String get baseUrl => _dio.options.baseUrl;
  set baseUrl(String value) => _dio.options.baseUrl = value;

  String? get sessionCookie => _sessionCookie;
  String? get csrfToken => _csrfToken;

  /// Applies a session cookie (from login, OIDC, or secure storage) and
  /// invalidates any cached CSRF token.
  void applySession(String? sessionCookie) {
    _sessionCookie = (sessionCookie == null || sessionCookie.isEmpty)
        ? null
        : sessionCookie;
    _csrfToken = null;
  }

  void _onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    final session = _sessionCookie;
    if (session != null && session.isNotEmpty) {
      options.headers['Cookie'] = 'session=$session';
    }
    final method = options.method.toUpperCase();
    final isMutation = method != 'GET' && method != 'HEAD' && method != 'OPTIONS';
    final token = _csrfToken;
    if (isMutation && token != null && token.isNotEmpty) {
      options.headers['X-CSRF-Token'] = token;
    }
    handler.next(options);
  }

  /// Fetches and caches the per-session CSRF token. No-op when unauthenticated.
  Future<String?> ensureCsrfToken() async {
    if (_csrfToken != null) return _csrfToken;
    if (_sessionCookie == null) return null;
    final data = await getJson('/api/csrf-token');
    if (data is Map && data['token'] != null) {
      _csrfToken = data['token'].toString();
    }
    return _csrfToken;
  }

  // --- Authentication -------------------------------------------------------

  Future<bool> checkSetup() async {
    final data = await getJson('/api/check-setup');
    return data is Map && data['setup'] == true;
  }

  Future<Map<String, dynamic>> fetchServerConfig() async {
    final data = await getJson('/api/config');
    return data is Map ? Map<String, dynamic>.from(data) : const {};
  }

  Future<LoginOutcome> login({
    required String username,
    required String password,
    bool setup = false,
  }) async {
    final body = <String, dynamic>{
      'username': username,
      'password': password,
      if (setup) 'setup': true,
    };
    try {
      final response = await _dio.post<dynamic>('/api/login', data: body);
      _sessionCookie = parseSessionCookie(response.headers['set-cookie']);
      _csrfToken = null;
      if (_sessionCookie != null) {
        await ensureCsrfToken();
      }
      return LoginOutcome(sessionCookie: _sessionCookie);
    } on DioException catch (error) {
      throw ApiException.fromDio(error);
    }
  }

  Future<void> logout() async {
    try {
      await _dio.post<dynamic>('/api/logout');
    } on DioException {
      // Best effort — the local session is cleared regardless.
    } finally {
      _sessionCookie = null;
      _csrfToken = null;
    }
  }

  // --- JSON transport -------------------------------------------------------

  Future<dynamic> getJson(String path, {Map<String, dynamic>? query}) =>
      _send(() => _dio.get<dynamic>(path, queryParameters: query));

  Future<dynamic> postJson(
    String path, {
    Object? body,
    Map<String, dynamic>? query,
  }) =>
      _send(() => _dio.post<dynamic>(path, data: body, queryParameters: query));

  Future<dynamic> deleteJson(String path, {Map<String, dynamic>? query}) =>
      _send(() => _dio.delete<dynamic>(path, queryParameters: query));

  /// Multipart upload. [mediaType] selects the server's file field name
  /// (`image`, `video`, or `audio`), matching `POST /api/upload`.
  Future<dynamic> uploadMedia({
    required String filePath,
    required String mediaType,
    String? filename,
    void Function(int sent, int total)? onProgress,
  }) async {
    final field = mediaType.isEmpty ? 'image' : mediaType;
    try {
      final multipart = await MultipartFile.fromFile(
        filePath,
        filename: filename ?? fileName(filePath),
        contentType: guessContentType(filePath) ?? MediaType('application', 'octet-stream'),
      );
      final form = FormData.fromMap({field: multipart, 'media_type': field});
      final response = await _dio.post<dynamic>(
        '/api/upload',
        data: form,
        onSendProgress: onProgress,
      );
      return response.data;
    } on DioException catch (error) {
      throw ApiException.fromDio(error);
    }
  }

  Future<dynamic> _send(Future<Response<dynamic>> Function() request) async {
    try {
      final response = await request();
      return response.data;
    } on DioException catch (error) {
      throw ApiException.fromDio(error);
    }
  }

  void close() => _dio.close(force: true);
}
