import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'features/auth/login_screen.dart';
import 'features/home/home_shell.dart';
import 'features/onboarding/server_url_screen.dart';
import 'state/providers.dart';
import 'theme.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const ProviderScope(child: TracesApp()));
}

/// Root application widget.
class TracesApp extends StatelessWidget {
  const TracesApp({super.key});

  @override
  Widget build(BuildContext context) => MaterialApp(
        title: 'TRACES',
        debugShowCheckedModeBanner: false,
        theme: TracesTheme.light(),
        darkTheme: TracesTheme.dark(),
        themeMode: ThemeMode.system,
        home: const AppRoot(),
      );
}

/// Chooses the top-level screen from the authentication state.
class AppRoot extends ConsumerWidget {
  const AppRoot({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authControllerProvider);

    if (auth.status == AuthStatus.needsServer) {
      return const ServerUrlScreen();
    }
    if (auth.status == AuthStatus.authenticated) {
      return const HomeShell();
    }
    // During the initial restore (or the first server handshake) show a splash;
    // later "loading" states keep the login screen mounted so inputs survive.
    if (auth.status == AuthStatus.loading &&
        (auth.serverUrl.isEmpty || auth.connecting)) {
      return const SplashScreen();
    }
    return const LoginScreen();
  }
}

/// Branded splash shown while restoring the persisted session.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.camera_roll_outlined, size: 64, color: scheme.primary),
            const SizedBox(height: 16),
            Text(
              'TRACES',
              style: Theme.of(context)
                  .textTheme
                  .headlineSmall
                  ?.copyWith(letterSpacing: 4),
            ),
            const SizedBox(height: 24),
            const SizedBox(
              width: 24,
              height: 24,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ],
        ),
      ),
    );
  }
}
