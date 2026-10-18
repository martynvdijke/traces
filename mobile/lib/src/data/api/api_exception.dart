import 'package:dio/dio.dart';

/// Error surfaced to the UI for any failed API interaction.
///
/// TRACES returns `{"error": "..."}` for failures; this unwraps that message
/// and keeps the HTTP status so callers can special-case 401 (session expiry).
class ApiException implements Exception {
  const ApiException(this.message, {this.statusCode});

  final String message;
  final int? statusCode;

  bool get isUnauthorized => statusCode == 401;

  factory ApiException.fromDio(DioException error) {
    final response = error.response;
    final data = response?.data;

    String message;
    if (data is Map && data['error'] != null) {
      message = data['error'].toString();
    } else if (response != null) {
      message = 'Request failed (HTTP ${response.statusCode})';
    } else {
      message = error.message ?? 'Could not reach the server';
    }
    return ApiException(message, statusCode: response?.statusCode);
  }

  @override
  String toString() => message;
}
