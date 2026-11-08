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

  test('manifest keeps the session out of Android backups', () {
    expect(manifest, contains('android:allowBackup="false"'));
    expect(manifest, contains('android:fullBackupContent="false"'));
    expect(
      manifest,
      contains('android:dataExtractionRules="@xml/data_extraction_rules"'),
    );

    final rules = File(
      'android/app/src/main/res/xml/data_extraction_rules.xml',
    );
    expect(rules.existsSync(), isTrue);
    final xml = rules.readAsStringSync();
    expect(xml, contains('<cloud-backup>'));
    expect(xml, contains('<device-transfer>'));
    for (final domain in [
      'root',
      'file',
      'database',
      'sharedpref',
      'external',
    ]) {
      expect(
        RegExp('<exclude domain="$domain" path="\\." ?/>')
            .allMatches(xml)
            .length,
        2,
        reason: 'domain $domain must be excluded from both backup paths',
      );
    }
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
