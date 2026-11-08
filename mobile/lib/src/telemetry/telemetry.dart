import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// Compile-time telemetry configuration.
///
/// Runtime OTLP settings normally come from `GET /api/otel-settings` (see
/// [Telemetry.startFromServer]); the dart-defines below are the fallback for
/// local builds and for endpoints that are known at build time.
abstract final class TelemetryConfig {
  static const otelEndpoint = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_ENDPOINT',
  );
  static const otelHeaders = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_HEADERS',
  );
  static const serviceName = String.fromEnvironment(
    'OTEL_SERVICE_NAME',
    defaultValue: 'traces-android',
  );
  static const serviceVersion = String.fromEnvironment(
    'OTEL_SERVICE_VERSION',
    defaultValue: '1.0.0',
  );
  static const umamiUrl = String.fromEnvironment('UMAMI_URL');
  static const umamiWebsiteId = String.fromEnvironment('UMAMI_WEBSITE_ID');

  static bool get otelEnabled => otelEndpoint.trim().isNotEmpty && !kIsWeb;
  static bool get umamiEnabled =>
      umamiUrl.trim().isNotEmpty && umamiWebsiteId.trim().isNotEmpty;
}

/// Runtime tracing settings returned by `GET /api/otel-settings`.
class OtelSettings {
  const OtelSettings({
    this.endpoint = '',
    this.tracesEnabled = false,
    this.metricsEnabled = false,
    this.logsEnabled = false,
  });

  final String endpoint;
  final bool tracesEnabled;
  final bool metricsEnabled;
  final bool logsEnabled;

  /// Tracing is only useful with both an endpoint and the traces flag set.
  bool get enabled => tracesEnabled && endpoint.trim().isNotEmpty;

  /// Tolerant decoder: the Go API and older deployments disagree on whether
  /// the endpoint is called `endpoint` or `otel_endpoint`, and on whether a
  /// master `enabled` flag exists.
  static OtelSettings fromJson(Map<String, dynamic> json) {
    final endpoint = _string(json, const ['endpoint', 'otel_endpoint']).trim();
    final master = _bool(json, const ['enabled', 'otel_enabled']);
    final traces = _bool(json, const ['traces_enabled']) ?? master ?? false;
    return OtelSettings(
      endpoint: endpoint,
      tracesEnabled: endpoint.isEmpty ? false : traces,
      metricsEnabled: _bool(json, const ['metrics_enabled']) ?? false,
      logsEnabled: _bool(json, const ['logs_enabled']) ?? false,
    );
  }

  static String _string(Map<String, dynamic> json, List<String> keys) {
    for (final key in keys) {
      final value = json[key];
      if (value is String) return value;
      if (value is num) return value.toString();
    }
    return '';
  }

  static bool? _bool(Map<String, dynamic> json, List<String> keys) {
    for (final key in keys) {
      if (!json.containsKey(key)) continue;
      final value = json[key];
      if (value is bool) return value;
      if (value is num) return value != 0;
      if (value is String) {
        switch (value.trim().toLowerCase()) {
          case 'true':
          case '1':
          case 'yes':
          case 'on':
            return true;
          case 'false':
          case '0':
          case 'no':
          case 'off':
            return false;
        }
      }
    }
    return null;
  }
}

/// Fetches the server's OTLP settings.
///
/// Never throws: offline devices, timeouts, malformed bodies and non-200
/// responses all return `null`, which leaves telemetry disabled.
Future<OtelSettings?> fetchOtelSettings(
  String baseUrl, {
  Dio? dio,
  Duration timeout = const Duration(seconds: 3),
}) async {
  final base = baseUrl.trim().replaceAll(RegExp(r'/+$'), '');
  if (base.isEmpty || kIsWeb) return null;

  final owned = dio == null;
  final client =
      dio ?? Dio(BaseOptions(connectTimeout: timeout, receiveTimeout: timeout));
  try {
    final response = await client
        .get<dynamic>(
          '$base/api/otel-settings',
          options: Options(
            responseType: ResponseType.json,
            sendTimeout: timeout,
            receiveTimeout: timeout,
            validateStatus: (_) => true,
            headers: const {'Accept': 'application/json'},
          ),
        )
        .timeout(timeout);
    if (response.statusCode != 200) return null;

    final data = response.data;
    final map = switch (data) {
      final Map<dynamic, dynamic> value => Map<String, dynamic>.from(value),
      final String value => _decodeMap(value),
      _ => null,
    };
    if (map == null) return null;
    return OtelSettings.fromJson(map);
  } on Object {
    return null;
  } finally {
    if (owned) client.close(force: true);
  }
}

Map<String, dynamic>? _decodeMap(String raw) {
  try {
    final decoded = jsonDecode(raw);
    return decoded is Map ? Map<String, dynamic>.from(decoded) : null;
  } on Object {
    return null;
  }
}

/// Allowlisted analytics events. Keys and values are enforced at the boundary,
/// so event titles, captions, people and searches can never be sent.
const Map<String, Set<String>> umamiEventAllowlist = {
  'screen_view': {'screen'},
  'sync_started': {},
  'theme_changed': {'theme'},
};

const Set<String> _screens = {
  'timeline',
  'capture',
  'map',
  'heatmap',
  'person',
  'compare',
  'settings',
  'login',
  'onboarding',
};
const Set<String> _themes = {'system', 'light', 'dark'};

Map<String, Object>? sanitizeUmamiEvent(
  String event, [
  Map<String, Object?> props = const {},
]) {
  final allowed = umamiEventAllowlist[event];
  if (allowed == null) return null;
  final clean = <String, Object>{};
  for (final entry in props.entries) {
    if (!allowed.contains(entry.key)) continue;
    final value = entry.value;
    if (value is bool || value is num) {
      clean[entry.key] = value as Object;
      continue;
    }
    if (value is! String || value.length > 64) continue;
    if (entry.key == 'screen' && !_screens.contains(value)) continue;
    if (entry.key == 'theme' && !_themes.contains(value)) continue;
    clean[entry.key] = value;
  }
  if (allowed.isNotEmpty && clean.isEmpty) return null;
  return clean;
}

String randomHex(int bytes) {
  final random = Random.secure();
  final buffer = StringBuffer();
  for (var i = 0; i < bytes; i++) {
    buffer.write(random.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return buffer.toString();
}

String buildTraceparent(String traceId, String spanId) =>
    '00-$traceId-$spanId-01';

class TraceSpan {
  TraceSpan({
    required this.name,
    required this.attributes,
    required this.traceId,
    required this.spanId,
    required this.startNanos,
    this.kind = 1,
  });

  final String name;
  final Map<String, Object> attributes;
  final String traceId;
  final String spanId;
  final int startNanos;
  final int kind;
  bool _ended = false;
  int? statusCode;
  String error = '';

  String get traceparent => buildTraceparent(traceId, spanId);

  void end({
    int? httpStatus,
    Object? failure,
    Map<String, Object> extra = const {},
  }) {
    if (_ended) return;
    _ended = true;
    statusCode = httpStatus;
    if (failure != null) error = failure.toString();
    final attrs = <String, Object>{...attributes, ...extra};
    if (httpStatus != null) attrs['http.response.status_code'] = httpStatus;
    if (error.isNotEmpty) attrs['error.message'] = _short(error);
    Telemetry.instance._finish(
      this,
      attrs,
      error.isNotEmpty || (httpStatus != null && httpStatus >= 500),
    );
  }

  String _short(String value) =>
      value.length > 200 ? '${value.substring(0, 200)}…' : value;
}

Map<String, Object?> buildOtlpPayload({
  required String serviceName,
  required String serviceVersion,
  required TraceSpan span,
  required Map<String, Object> attributes,
  required bool failed,
  required int endNanos,
}) {
  List<Map<String, Object?>> attrs(Map<String, Object> values) => values.entries
      .map(
        (entry) => {
          'key': entry.key,
          'value': switch (entry.value) {
            final bool value => {'boolValue': value},
            final int value => {'intValue': value.toString()},
            final double value => {'doubleValue': value},
            _ => {'stringValue': entry.value.toString()},
          },
        },
      )
      .toList();

  return {
    'resourceSpans': [
      {
        'resource': {
          'attributes': attrs({
            'service.name': serviceName,
            'service.version': serviceVersion,
            'telemetry.sdk.name': 'traces-mobile',
            'telemetry.sdk.language': 'dart',
          }),
        },
        'scopeSpans': [
          {
            'scope': {'name': 'traces.mobile', 'version': serviceVersion},
            'spans': [
              {
                'traceId': span.traceId,
                'spanId': span.spanId,
                'name': span.name,
                'kind': span.kind,
                'startTimeUnixNano': span.startNanos.toString(),
                'endTimeUnixNano': endNanos.toString(),
                'attributes': attrs(attributes),
                'status': {'code': failed ? 2 : 1},
              },
            ],
          },
        ],
      },
    ],
  };
}

/// Best-effort OTLP/HTTP JSON exporter plus Umami events.
///
/// Exports are queued in memory and flushed every 5 seconds (or as soon as 20
/// spans are queued). All network work is fire-and-forget so tracing can never
/// block the UI or surface errors to the user.
class Telemetry {
  Telemetry._();

  static final Telemetry instance = Telemetry._();

  Dio? _dio;
  Timer? _timer;
  final List<Map<String, Object?>> _queue = [];
  bool _started = false;
  String _tracesEndpoint = '';
  Map<String, String> _headers = const {};

  bool get otelEnabled => _tracesEndpoint.isNotEmpty;
  bool get umamiEnabled => TelemetryConfig.umamiEnabled;

  /// Starts exporting to [endpoint] (or the compile-time fallback).
  ///
  /// No-op when tracing is already running, on web, or when no endpoint is
  /// configured. The first span (`app.start`) is recorded once started.
  Future<void> start({String? endpoint, Map<String, String>? headers}) async {
    if (_started || kIsWeb) return;
    final raw = (endpoint ?? TelemetryConfig.otelEndpoint).trim();
    if (raw.isEmpty) return;
    _started = true;
    _dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 5),
        receiveTimeout: const Duration(seconds: 5),
        sendTimeout: const Duration(seconds: 5),
      ),
    );
    _tracesEndpoint = normalizeEndpoint(raw);
    _headers = headers ?? parseOtelHeaders(TelemetryConfig.otelHeaders);
    _timer = Timer.periodic(const Duration(seconds: 5), (_) => _flush());
    final span = beginSpan('app.start');
    span.end();
  }

  /// Fetches the OTLP endpoint from `baseUrl/api/otel-settings` and starts.
  ///
  /// Never throws and never blocks first paint: call it with `unawaited`.
  Future<void> startFromServer(String baseUrl) async {
    if (_started || kIsWeb) return;
    final settings = await fetchOtelSettings(baseUrl);
    if (settings == null || !settings.enabled) return;
    await start(endpoint: settings.endpoint);
  }

  TraceSpan beginSpan(
    String name, {
    Map<String, Object> attributes = const {},
    int kind = 1,
  }) {
    return TraceSpan(
      name: name,
      attributes: attributes,
      traceId: randomHex(16),
      spanId: randomHex(8),
      startNanos: DateTime.now().microsecondsSinceEpoch * 1000,
      kind: kind,
    );
  }

  void screen(String name) {
    final span = beginSpan('screen $name', attributes: {'screen.name': name});
    span.end();
    track('screen_view', {'screen': name});
  }

  void track(String event, [Map<String, Object?> props = const {}]) {
    final clean = sanitizeUmamiEvent(event, props);
    if (clean == null || !TelemetryConfig.umamiEnabled) return;
    final client = _dio ??= Dio();
    final base = TelemetryConfig.umamiUrl.replaceAll(RegExp(r'/+$'), '');
    final uri = Uri.tryParse('$base/api/send');
    if (uri == null) return;
    final body = {
      'type': 'event',
      'payload': {
        'website': TelemetryConfig.umamiWebsiteId,
        'hostname': 'traces.app',
        'url': '/app/$event',
        'event': event,
        'data': clean,
      },
    };
    unawaited(
      client
          .post<dynamic>(
            uri.toString(),
            data: jsonEncode(body),
            options: Options(
              contentType: Headers.jsonContentType,
              validateStatus: (_) => true,
            ),
          )
          .then((_) {}, onError: (Object _) {}),
    );
  }

  void _finish(TraceSpan span, Map<String, Object> attributes, bool failed) {
    if (!otelEnabled) return;
    final payload = buildOtlpPayload(
      serviceName: TelemetryConfig.serviceName,
      serviceVersion: TelemetryConfig.serviceVersion,
      span: span,
      attributes: attributes,
      failed: failed,
      endNanos: DateTime.now().microsecondsSinceEpoch * 1000,
    );
    _queue.add(payload);
    if (_queue.length >= 20) _flush();
  }

  void _flush() {
    final client = _dio;
    if (_queue.isEmpty || client == null || _tracesEndpoint.isEmpty) return;
    final spans = <Map<String, Object?>>[];
    for (final payload in _queue) {
      final resourceSpans = payload['resourceSpans'];
      if (resourceSpans is List && resourceSpans.isNotEmpty) {
        final resource = resourceSpans.first;
        if (resource is Map) {
          final scopeSpans = resource['scopeSpans'];
          if (scopeSpans is List && scopeSpans.isNotEmpty) {
            final scope = scopeSpans.first;
            if (scope is Map && scope['spans'] is List) {
              spans.addAll(
                (scope['spans'] as List).cast<Map<String, Object?>>(),
              );
            }
          }
        }
      }
    }
    _queue.clear();
    if (spans.isEmpty) return;
    final payload = {
      'resourceSpans': [
        {
          'resource': {
            'attributes': [
              {
                'key': 'service.name',
                'value': {'stringValue': TelemetryConfig.serviceName},
              },
              {
                'key': 'service.version',
                'value': {'stringValue': TelemetryConfig.serviceVersion},
              },
            ],
          },
          'scopeSpans': [
            {
              'scope': {'name': 'traces.mobile'},
              'spans': spans,
            },
          ],
        },
      ],
    };
    unawaited(
      client
          .post<dynamic>(
            _tracesEndpoint,
            data: jsonEncode(payload),
            options: Options(
              contentType: Headers.jsonContentType,
              headers: _headers,
              validateStatus: (_) => true,
            ),
          )
          .then(
            (response) {
              final status = response.statusCode ?? 0;
              if (status >= 400 && kDebugMode) {
                debugPrint('OTLP export failed: $status');
              }
            },
            onError: (Object error) {
              if (kDebugMode) debugPrint('OTLP export failed: $error');
            },
          ),
    );
  }

  Future<void> shutdown() async {
    _timer?.cancel();
    _timer = null;
    _flush();
    await Future<void>.delayed(const Duration(milliseconds: 200));
    _dio?.close(force: true);
    _dio = null;
    _started = false;
    _tracesEndpoint = '';
  }

  /// Appends `/v1/traces` unless the URL already points at the traces path.
  static String normalizeEndpoint(String raw) {
    final base = raw.trim().replaceAll(RegExp(r'/+$'), '');
    if (base.isEmpty) return '';
    if (base.endsWith('/v1/traces')) return base;
    return '$base/v1/traces';
  }

  /// Parses the `key=value,key2=value2` OTLP headers format.
  static Map<String, String> parseOtelHeaders(String raw) {
    final out = <String, String>{};
    for (final pair in raw.split(',')) {
      final idx = pair.indexOf('=');
      if (idx <= 0) continue;
      final key = pair.substring(0, idx).trim();
      var value = pair.substring(idx + 1).trim();
      try {
        value = Uri.decodeComponent(value);
      } catch (_) {}
      if (key.isNotEmpty && value.isNotEmpty) out[key] = value;
    }
    return out;
  }
}
