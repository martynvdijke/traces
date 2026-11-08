import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/features/auth/login_screen.dart';
import 'package:traces/src/state/providers.dart';

class _DemoAuthController extends AuthController {
  _DemoAuthController({this.credentials = true});

  final bool credentials;

  @override
  AuthState build() => AuthState(
        status: AuthStatus.needsLogin,
        serverUrl: 'https://traces.test',
        demoEnabled: true,
        demoUsername: credentials ? 'reviewer' : '',
        demoPassword: credentials ? 'secret-demo-pass' : '',
      );
}

Future<void> _pumpLogin(
  WidgetTester tester, {
  AuthController Function() create = _DemoAuthController.new,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [authControllerProvider.overrideWith(create)],
      child: const MaterialApp(home: LoginScreen()),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('demo servers offer the shortcut without exposing credentials',
      (tester) async {
    await _pumpLogin(tester);

    expect(find.byKey(const Key('login-demo')), findsOneWidget);
    expect(find.text('Try the demo account'), findsOneWidget);
    expect(
      find.text('Explore the shared demo server with sample memories.'),
      findsOneWidget,
    );
    expect(find.text('secret-demo-pass'), findsNothing);
    expect(find.byKey(const Key('login-submit')), findsOneWidget);
  });

  testWidgets('ordinary servers do not show the demo shortcut', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          authControllerProvider.overrideWith(_PlainAuthController.new),
        ],
        child: const MaterialApp(home: LoginScreen()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('login-demo')), findsNothing);
  });

  test('a demo login without stored credentials fails with a clear message',
      () async {
    final container = ProviderContainer(
      overrides: [
        authControllerProvider.overrideWith(
          () => _DemoAuthController(credentials: false),
        ),
      ],
    );
    addTearDown(container.dispose);

    final signedIn =
        await container.read(authControllerProvider.notifier).signInWithDemo();

    expect(signedIn, isFalse);
    expect(
      container.read(authControllerProvider).error,
      'Demo sign-in is not available on this server.',
    );
  });
}

class _PlainAuthController extends AuthController {
  @override
  AuthState build() => const AuthState(
        status: AuthStatus.needsLogin,
        serverUrl: 'https://traces.test',
      );
}
