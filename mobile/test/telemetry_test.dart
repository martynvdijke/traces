import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/telemetry/telemetry.dart';

/// Minimal [HttpClientAdapter] stand-in so no real socket is opened.
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.handler);

  final Future<ResponseBody> Function(RequestOptions options) handler;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) {
    requests.add(options);
    return handler(options);
  }

  @override
  void close({bool force = false}) {}
}

Dio _dioWith(_FakeAdapter adapter) => Dio()..httpClientAdapter = adapter;

void main() {
  group('normalizeEndpoint', () {
    test('appends the OTLP traces path', () {
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.com'),
        'https://otel.example.com/v1/traces',
      );
    });

    test('trims whitespace and trailing slashes', () {
      expect(
        Telemetry.normalizeEndpoint('  https://otel.example.com///  '),
        'https://otel.example.com/v1/traces',
      );
    });

    test('is idempotent for an endpoint that already ends in /v1/traces', () {
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.com/v1/traces'),
        'https://otel.example.com/v1/traces',
      );
    });

    test('returns an empty string for empty input', () {
      expect(Telemetry.normalizeEndpoint('   '), '');
    });
  });

  group('parseOtelHeaders', () {
    test('parses comma-separated key=value pairs', () {
      expect(
        Telemetry.parseOtelHeaders('Authorization=Bearer abc,X-Env=prod'),
        {'Authorization': 'Bearer abc', 'X-Env': 'prod'},
      );
    });

    test('URL-decodes values', () {
      expect(
        Telemetry.parseOtelHeaders('Authorization=Bearer%20token%3D1, X=1'),
        {'Authorization': 'Bearer token=1', 'X': '1'},
      );
    });

    test('skips malformed pairs and empty values', () {
      expect(Telemetry.parseOtelHeaders('noequals,=novalue,empty=,ok=yes'), {
        'ok': 'yes',
      });
    });

    test('returns an empty map for empty input', () {
      expect(Telemetry.parseOtelHeaders(''), isEmpty);
    });
  });

  group('OtelSettings.fromJson', () {
    test('reads the canonical API shape', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': 'https://otlp.example.com',
        'traces_enabled': true,
        'metrics_enabled': false,
        'logs_enabled': true,
      });
      expect(settings.endpoint, 'https://otlp.example.com');
      expect(settings.tracesEnabled, isTrue);
      expect(settings.metricsEnabled, isFalse);
      expect(settings.logsEnabled, isTrue);
      expect(settings.enabled, isTrue);
    });

    test('accepts the otel_endpoint / otel_enabled aliases', () {
      final settings = OtelSettings.fromJson(const {
        'otel_endpoint': 'https://otlp.example.com',
        'otel_enabled': true,
      });
      expect(settings.endpoint, 'https://otlp.example.com');
      expect(settings.tracesEnabled, isTrue);
    });

    test(
      'falls back to the master enabled flag when traces_enabled is absent',
      () {
        final settings = OtelSettings.fromJson(const {
          'endpoint': 'https://otlp.example.com',
          'enabled': true,
        });
        expect(settings.tracesEnabled, isTrue);
      },
    );

    test('explicit traces_enabled wins over the master flag', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': 'https://otlp.example.com',
        'enabled': true,
        'traces_enabled': false,
      });
      expect(settings.tracesEnabled, isFalse);
      expect(settings.enabled, isFalse);
    });

    test('treats an empty endpoint as disabled', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': '  ',
        'traces_enabled': true,
      });
      expect(settings.endpoint, isEmpty);
      expect(settings.tracesEnabled, isFalse);
      expect(settings.enabled, isFalse);
    });

    test('tolerates numeric and string booleans', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': 'https://otlp.example.com',
        'traces_enabled': 1,
        'metrics_enabled': 'true',
        'logs_enabled': '0',
      });
      expect(settings.tracesEnabled, isTrue);
      expect(settings.metricsEnabled, isTrue);
      expect(settings.logsEnabled, isFalse);
    });

    test('missing fields default to disabled', () {
      final settings = OtelSettings.fromJson(const {});
      expect(settings.endpoint, isEmpty);
      expect(settings.tracesEnabled, isFalse);
      expect(settings.metricsEnabled, isFalse);
      expect(settings.logsEnabled, isFalse);
    });
  });

  group('fetchOtelSettings', () {
    test('GETs /api/otel-settings on the supplied base URL', () async {
      final adapter = _FakeAdapter(
        (_) async => ResponseBody.fromString(
          jsonEncode({
            'endpoint': 'https://otlp.example.com',
            'traces_enabled': true,
            'metrics_enabled': false,
            'logs_enabled': false,
          }),
          200,
          headers: {
            Headers.contentTypeHeader: [Headers.jsonContentType],
          },
        ),
      );

      final settings = await fetchOtelSettings(
        'https://traces.example.com/',
        dio: _dioWith(adapter),
      );

      expect(adapter.requests, hasLength(1));
      expect(
        adapter.requests.single.uri.toString(),
        'https://traces.example.com/api/otel-settings',
      );
      expect(settings, isNotNull);
      expect(settings!.endpoint, 'https://otlp.example.com');
      expect(settings.enabled, isTrue);
    });

    test('returns null on a non-200 response', () async {
      final adapter = _FakeAdapter(
        (_) async => ResponseBody.fromString('nope', 500),
      );
      expect(
        await fetchOtelSettings(
          'https://traces.example.com',
          dio: _dioWith(adapter),
        ),
        isNull,
      );
    });

    test('returns null instead of throwing when the request fails', () async {
      final adapter = _FakeAdapter(
        (options) async =>
            throw DioException(requestOptions: options, error: 'offline'),
      );
      expect(
        await fetchOtelSettings(
          'https://traces.example.com',
          dio: _dioWith(adapter),
        ),
        isNull,
      );
    });

    test('returns null for a malformed body', () async {
      final adapter = _FakeAdapter(
        (_) async => ResponseBody.fromString('<html>oops</html>', 200),
      );
      expect(
        await fetchOtelSettings(
          'https://traces.example.com',
          dio: _dioWith(adapter),
        ),
        isNull,
      );
    });

    test('does not call the network for an empty base URL', () async {
      final adapter = _FakeAdapter(
        (_) async => ResponseBody.fromString('{}', 200),
      );
      expect(await fetchOtelSettings('  ', dio: _dioWith(adapter)), isNull);
      expect(adapter.requests, isEmpty);
    });
  });

  group('buildOtlpPayload', () {
    test('emits traces.mobile resource and span attributes', () {
      final span = TraceSpan(
        name: 'screen timeline',
        attributes: const {'screen.name': 'timeline'},
        traceId: 'a' * 32,
        spanId: 'b' * 16,
        startNanos: 1000,
      );

      final payload = buildOtlpPayload(
        serviceName: 'traces-android',
        serviceVersion: '2.3.2',
        span: span,
        attributes: const {
          'screen.name': 'timeline',
          'http.response.status_code': 200,
          'count': 3,
          'ratio': 0.5,
          'cached': true,
        },
        failed: false,
        endNanos: 2000,
      );

      final resourceSpans = payload['resourceSpans'] as List;
      final resource = resourceSpans.single as Map;
      final resourceAttrs = {
        for (final attr in resource['resource']['attributes'] as List)
          attr['key']: attr['value'],
      };
      expect(resourceAttrs['service.name'], {'stringValue': 'traces-android'});
      expect(resourceAttrs['telemetry.sdk.name'], {
        'stringValue': 'traces-mobile',
      });

      final scopeSpans = resource['scopeSpans'] as List;
      final scope = scopeSpans.single as Map;
      expect(scope['scope']['name'], 'traces.mobile');

      final emitted = (scope['spans'] as List).single as Map;
      expect(emitted['traceId'], 'a' * 32);
      expect(emitted['spanId'], 'b' * 16);
      expect(emitted['name'], 'screen timeline');
      expect(emitted['startTimeUnixNano'], '1000');
      expect(emitted['endTimeUnixNano'], '2000');
      expect(emitted['status'], {'code': 1});

      final attrs = {
        for (final attr in emitted['attributes'] as List)
          attr['key']: attr['value'],
      };
      expect(attrs['count'], {'intValue': '3'});
      expect(attrs['ratio'], {'doubleValue': 0.5});
      expect(attrs['cached'], {'boolValue': true});
      expect(attrs['screen.name'], {'stringValue': 'timeline'});
    });

    test('marks failed spans with status code 2', () {
      final span = TraceSpan(
        name: 'request',
        attributes: const {},
        traceId: 'c' * 32,
        spanId: 'd' * 16,
        startNanos: 1,
      );
      final payload = buildOtlpPayload(
        serviceName: 'traces-android',
        serviceVersion: '1.0.0',
        span: span,
        attributes: const {},
        failed: true,
        endNanos: 2,
      );
      final emitted =
          ((payload['resourceSpans'] as List).single as Map)['scopeSpans']
              as List;
      final spanOut = ((emitted.single as Map)['spans'] as List).single as Map;
      expect(spanOut['status'], {'code': 2});
    });
  });
}
