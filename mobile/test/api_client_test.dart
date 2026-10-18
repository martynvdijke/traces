import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/api/api_client.dart';

void main() {
  group('parseSetCookie', () {
    test('extracts the session cookie value from Set-Cookie headers', () {
      expect(
        parseSessionCookie([
          'session=abc123; Path=/; HttpOnly; Max-Age=86400; SameSite=Lax',
        ]),
        'abc123',
      );
    });

    test('finds the session cookie among other Set-Cookie headers', () {
      expect(
        parseSetCookie(['theme=dark; Path=/', 'session=zzz; Path=/'], 'session'),
        'zzz',
      );
    });

    test('returns null when the cookie is absent', () {
      expect(parseSessionCookie(['other=1; Path=/']), isNull);
      expect(parseSessionCookie(null), isNull);
      expect(parseSessionCookie(const []), isNull);
    });

    test('returns null when the cookie was cleared', () {
      expect(parseSessionCookie(['session=; Path=/; Max-Age=0']), isNull);
    });
  });

  group('parseCookieHeader', () {
    test('finds a cookie inside a request Cookie header', () {
      expect(parseCookieHeader('a=1; session=xyz789; b=2', 'session'), 'xyz789');
    });

    test('trims surrounding whitespace', () {
      expect(parseCookieHeader('  session = spaced  ', 'session'), 'spaced');
    });

    test('returns null for missing or empty input', () {
      expect(parseCookieHeader(null, 'session'), isNull);
      expect(parseCookieHeader('', 'session'), isNull);
      expect(parseCookieHeader('a=1; b=2', 'session'), isNull);
      expect(parseCookieHeader('session=', 'session'), isNull);
    });
  });
}
