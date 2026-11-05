import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  final manifest =
      File('android/app/src/main/AndroidManifest.xml').readAsStringSync();
  final gradle = File('android/app/build.gradle.kts').readAsStringSync();

  test('manifest grants INTERNET', () {
    expect(manifest, contains('android.permission.INTERNET'));
  });

  test('manifest permits self-hosted cleartext HTTP', () {
    expect(manifest, contains('android:usesCleartextTraffic="true"'));
  });

  test('release build wires key.properties signing with a debug fallback', () {
    expect(gradle, contains('key.properties'));
    expect(gradle, contains('releaseSigningConfig'));
    expect(gradle, contains('signingConfigs.getByName("debug")'));
  });

  test('launcher activity source is committed and correctly packaged', () {
    final activity = RegExp(r'android:name="\.([A-Za-z0-9_]+)"')
        .firstMatch(manifest)
        ?.group(1);
    expect(activity, isNotNull,
        reason: 'AndroidManifest must declare a launcher activity');

    final namespace = RegExp(r'namespace\s*=\s*"([^"]+)"')
        .firstMatch(gradle)
        ?.group(1);
    expect(namespace, isNotNull, reason: 'build.gradle.kts must set a namespace');

    final source = File(
      'android/app/src/main/kotlin/${namespace!.replaceAll('.', '/')}/$activity.kt',
    );
    expect(
      source.existsSync(),
      isTrue,
      reason: 'The launcher Activity source must be committed; a missing '
          '$activity.kt builds fine but crashes on launch with '
          'ActivityNotFoundException (an unanchored "traces" entry in the root '
          '.gitignore used to hide it from git).',
    );
    expect(source.readAsStringSync(), contains('class $activity'));
  });
}
