import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/api/api_client.dart';
import 'package:traces/src/data/api/api_exception.dart';
import 'package:traces/src/features/settings/settings_screen.dart';
import 'package:traces/src/state/providers.dart';
import 'package:traces/src/theme.dart';

class _FakeApiClient extends ApiClient {
  final Map<String, dynamic> privacy = const {'analytics_opt_out': false};
  final List<bool> privacyUpdates = <bool>[];
  bool deleted = false;
  Object? deleteError;

  @override
  Future<Map<String, dynamic>> fetchPrivacySettings() async => privacy;

  @override
  Future<void> updatePrivacySettings({required bool analyticsOptOut}) async {
    privacyUpdates.add(analyticsOptOut);
  }

  @override
  Future<Map<String, dynamic>> exportAccount() async => {
    'account': {'username': 'demo'},
    'events': [],
    'persons': [],
    'event_templates': [],
    'preferences': {'analytics_opt_out': false},
  };

  @override
  Future<void> deleteAccount() async {
    final error = deleteError;
    if (error != null) throw error;
    deleted = true;
  }
}

class _FakeAuthController extends AuthController {
  bool signedOutAfterDeletion = false;

  @override
  AuthState build() => const AuthState(
    status: AuthStatus.authenticated,
    serverUrl: 'https://traces.test',
    username: 'demo',
    demoEnabled: true,
    demoUsername: 'demo',
    demoPassword: 'demo-password',
  );

  @override
  Future<void> signOutAfterDeletion() async {
    signedOutAfterDeletion = true;
  }
}

Future<_FakeApiClient> _pumpSettings(
  WidgetTester tester, {
  _FakeApiClient? client,
}) async {
  final api = client ?? _FakeApiClient();
  tester.view.physicalSize = const Size(900, 1800);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(api),
        authControllerProvider.overrideWith(_FakeAuthController.new),
      ],
      child: MaterialApp(
        theme: TracesTheme.light(),
        home: const SettingsScreen(),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return api;
}

void main() {
  testWidgets('settings shows the account, privacy, and legal actions', (
    tester,
  ) async {
    await _pumpSettings(tester);

    expect(find.text('Settings'), findsOneWidget);
    expect(find.byKey(const Key('settings-username')), findsOneWidget);
    expect(find.text('demo'), findsOneWidget);
    expect(find.text('https://traces.test'), findsOneWidget);
    expect(find.byKey(const Key('settings-sign-out')), findsOneWidget);
    expect(find.byKey(const Key('settings-export')), findsOneWidget);
    expect(find.byKey(const Key('settings-analytics')), findsOneWidget);
    expect(find.byKey(const Key('settings-delete')), findsOneWidget);
    expect(find.byKey(const Key('settings-privacy-policy')), findsOneWidget);
    expect(find.byKey(const Key('settings-terms')), findsOneWidget);
    expect(find.byKey(const Key('settings-delete-page')), findsOneWidget);
    expect(find.text('Allow anonymous analytics'), findsOneWidget);
  });

  testWidgets('toggling analytics saves the new preference', (tester) async {
    final api = await _pumpSettings(tester);

    expect(
      tester
          .widget<SwitchListTile>(
            find.byKey(const Key('settings-analytics')),
          )
          .value,
      isTrue,
    );

    await tester.tap(find.byKey(const Key('settings-analytics')));
    await tester.pumpAndSettle();

    expect(api.privacyUpdates, [true]);
    expect(
      tester
          .widget<SwitchListTile>(
            find.byKey(const Key('settings-analytics')),
          )
          .value,
      isFalse,
    );
  });

  testWidgets('export sheet shows the JSON size and copies it', (tester) async {
    await _pumpSettings(tester);

    await tester.tap(find.byKey(const Key('settings-export')));
    await tester.pumpAndSettle();

    expect(find.textContaining('JSON document is ready'), findsOneWidget);
    expect(find.byKey(const Key('settings-export-copy')), findsOneWidget);
  });

  testWidgets('deleting an account requires typing DELETE', (tester) async {
    final api = await _pumpSettings(tester);

    await tester.tap(find.byKey(const Key('settings-delete')));
    await tester.pumpAndSettle();

    expect(find.text('Delete your account?'), findsOneWidget);
    expect(
      tester
          .widget<FilledButton>(
            find.byKey(const Key('settings-delete-confirm')),
          )
          .onPressed,
      isNull,
    );

    await tester.enterText(
      find.byKey(const Key('settings-delete-field')),
      'DELETE',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('settings-delete-confirm')));
    await tester.pumpAndSettle();

    expect(api.deleted, isTrue);
  });

  testWidgets('a last-administrator error is explained in the dialog', (
    tester,
  ) async {
    final api = _FakeApiClient()
      ..deleteError = const ApiException('conflict', statusCode: 409);
    await _pumpSettings(tester, client: api);

    await tester.tap(find.byKey(const Key('settings-delete')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('settings-delete-field')),
      'DELETE',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('settings-delete-confirm')));
    await tester.pumpAndSettle();

    expect(find.textContaining('last administrator'), findsOneWidget);
    expect(api.deleted, isFalse);
  });
}
