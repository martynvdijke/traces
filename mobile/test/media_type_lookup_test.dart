import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/api/media_type_lookup.dart';

void main() {
  group('fileName / extensionOf', () {
    test('extracts the basename and lower-cased extension', () {
      expect(fileName('/tmp/photos/IMG_1234.JPG'), 'IMG_1234.JPG');
      expect(fileName(r'C:\Users\me\beach.png'), 'beach.png');
      expect(extensionOf('/tmp/IMG_1234.JPG'), 'jpg');
    });

    test('handles paths without an extension', () {
      expect(extensionOf('/tmp/README'), '');
      expect(extensionOf(''), '');
    });
  });

  group('guessContentType', () {
    test('maps images to image/*', () {
      expect(guessContentType('a.jpg').toString(), 'image/jpeg');
      expect(guessContentType('a.jpeg').toString(), 'image/jpeg');
      expect(guessContentType('a.png').toString(), 'image/png');
      expect(guessContentType('a.webp').toString(), 'image/webp');
    });

    test('maps videos to video/*', () {
      expect(guessContentType('a.mp4').toString(), 'video/mp4');
      expect(guessContentType('a.mov').toString(), 'video/quicktime');
      expect(guessContentType('a.3gp').toString(), 'video/3gpp');
    });

    test('maps audio to audio/*', () {
      expect(guessContentType('a.mp3').toString(), 'audio/mpeg');
      expect(guessContentType('a.m4a').toString(), 'audio/mp4');
      expect(guessContentType('a.ogg').toString(), 'audio/ogg');
    });

    test('returns null for unknown extensions', () {
      expect(guessContentType('a.txt'), isNull);
      expect(guessContentType('noextension'), isNull);
    });
  });
}
