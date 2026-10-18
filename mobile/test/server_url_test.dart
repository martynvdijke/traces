import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/api/server_url.dart';

void main() {
  group('normalizeServerUrl', () {
    test('prepends http:// when no scheme is given', () {
      expect(
        normalizeServerUrl('traces.example.com'),
        'http://traces.example.com',
      );
    });

    test('keeps an explicit scheme', () {
      expect(
        normalizeServerUrl('https://traces.example.com'),
        'https://traces.example.com',
      );
      expect(
        normalizeServerUrl('http://192.168.1.10:6270'),
        'http://192.168.1.10:6270',
      );
    });

    test('trims whitespace and strips trailing slashes', () {
      expect(normalizeServerUrl('  https://a.test/  '), 'https://a.test');
      expect(normalizeServerUrl('http://a.test///'), 'http://a.test');
    });

    test('returns an empty string for blank input', () {
      expect(normalizeServerUrl('   '), '');
    });
  });

  group('isValidServerUrl', () {
    test('accepts http and https hosts', () {
      expect(isValidServerUrl('https://traces.example.com'), isTrue);
      expect(isValidServerUrl('http://10.0.0.2:6270'), isTrue);
    });

    test('rejects unsupported schemes and empty values', () {
      expect(isValidServerUrl('ftp://traces.example.com'), isFalse);
      expect(isValidServerUrl(''), isFalse);
      expect(isValidServerUrl('   '), isFalse);
    });
  });

  group('absoluteUrl', () {
    test('joins a relative media path onto the base URL', () {
      expect(
        absoluteUrl('https://a.test', '/media/ab/x.jpg'),
        'https://a.test/media/ab/x.jpg',
      );
    });

    test('tolerates a trailing slash on the base URL', () {
      expect(
        absoluteUrl('https://a.test/', '/media/x.jpg'),
        'https://a.test/media/x.jpg',
      );
    });

    test('adds a leading slash when the path has none', () {
      expect(
        absoluteUrl('https://a.test', 'media/x.jpg'),
        'https://a.test/media/x.jpg',
      );
    });

    test('passes through absolute URLs untouched', () {
      expect(
        absoluteUrl('https://a.test', 'https://cdn.test/x.jpg'),
        'https://cdn.test/x.jpg',
      );
    });

    test('returns the path unchanged without a base URL', () {
      expect(absoluteUrl('', '/media/x.jpg'), '/media/x.jpg');
      expect(absoluteUrl('https://a.test', ''), '');
    });
  });
}
