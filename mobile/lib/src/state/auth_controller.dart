import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/api/api_client.dart';
import '../data/api/api_exception.dart';
import '../data/api/server_url.dart';
import '../data/models/server_config.dart';
import '../data/storage/app_storage.dart';
import 'auth_state.dart';
import 'storage_providers.dart';

/// Owns the [_client], the persisted server URL, and the `session` cookie.
///
/// The server authenticates exclusively with an HttpOnly `session` cookie, so
/// the cookie is captured from the login response and persisted in secure
/// storage (there is no bearer token to store).
class AuthController extends Notifier<AuthState> {
  final ApiClient client = ApiClient();

  late final AppStorage _storage;
  bool _restoreStarted = false;

  @override
  AuthState build() {
    _storage = ref.watch(appStorageProvider);
    if (!_restoreStarted) {
      _restoreStarted = true;
      scheduleMicrotask(_restore);
    }
    return const AuthState(status: AuthStatus.loading);
  }

  /// Human-readable OIDC entry point for the in-app WebView.
  String get oidcLoginUrl => '${state.serverUrl}/api/auth/oidc/login';

  Future<void> _restore() async {
    final url = await _storage.readServerUrl();
    if (url == null || url.isEmpty) {
      state = const AuthState(status: AuthStatus.needsServer);
      return;
    }

    client.baseUrl = url;
    final cookie = await _storage.readSessionCookie();
    final username = await _storage.readUsername() ?? '';
    final oidcEnabled = await _storage.readOidcEnabled();

    if (cookie == null || cookie.isEmpty) {
      state = AuthState(
        status: AuthStatus.needsLogin,
        serverUrl: url,
        username: username,
        oidcEnabled: oidcEnabled,
      );
      return;
    }

    client.applySession(cookie);
    try {
      await client.ensureCsrfToken();
      state = AuthState(
        status: AuthStatus.authenticated,
        serverUrl: url,
        username: username,
        oidcEnabled: oidcEnabled,
      );
    } on ApiException catch (error) {
      if (error.isUnauthorized) {
        await _storage.clearSessionCookie();
      }
      state = AuthState(
        status: AuthStatus.needsLogin,
        serverUrl: url,
        username: username,
        oidcEnabled: oidcEnabled,
        error: error.isUnauthorized ? null : error.message,
      );
    }
  }

  /// Validates and persists a new server address, then probes `/api/config`
  /// and `/api/check-setup`.
  Future<void> connectToServer(String input) async {
    final normalized = normalizeServerUrl(input);
    if (!isValidServerUrl(normalized)) {
      state = state.copyWith(
        status: AuthStatus.needsServer,
        error: 'Enter a valid server address, e.g. https://traces.example.com',
      );
      return;
    }

    state = AuthState(
      status: AuthStatus.loading,
      serverUrl: normalized,
      connecting: true,
    );
    client.baseUrl = normalized;

    try {
      final config = ServerConfig.fromJson(await client.fetchServerConfig());
      final setupRequired = await client.checkSetup();
      await _storage.writeServerUrl(normalized);
      await _storage.writeOidcEnabled(config.oidcEnabled);
      state = AuthState(
        status: AuthStatus.needsLogin,
        serverUrl: normalized,
        oidcEnabled: config.oidcEnabled,
        setupRequired: setupRequired,
      );
    } on ApiException catch (error) {
      state = AuthState(
        status: AuthStatus.needsServer,
        serverUrl: normalized,
        error: 'Could not reach $normalized — ${error.message}',
      );
    }
  }

  Future<void> forgetServer() async {
    client.applySession(null);
    await _storage.clearSessionCookie();
    state = const AuthState(status: AuthStatus.needsServer);
  }

  /// Username/password login (also performs first-run admin creation when the
  /// server reports no users yet).
  Future<bool> signIn({required String username, required String password}) async {
    final trimmed = username.trim();
    if (trimmed.isEmpty || password.isEmpty) {
      state = state.copyWith(
        status: AuthStatus.needsLogin,
        error: 'Enter your username and password',
      );
      return false;
    }

    state = state.copyWith(status: AuthStatus.loading, clearError: true);
    try {
      final outcome = await client.login(
        username: trimmed,
        password: password,
        setup: state.setupRequired,
      );
      final cookie = outcome.sessionCookie ?? client.sessionCookie;
      if (cookie == null || cookie.isEmpty) {
        state = state.copyWith(
          status: AuthStatus.needsLogin,
          error: 'The server did not return a session. Please try again.',
        );
        return false;
      }

      await _storage.writeSessionCookie(cookie);
      await _storage.writeUsername(trimmed);
      state = state.copyWith(
        status: AuthStatus.authenticated,
        username: trimmed,
        setupRequired: false,
        clearError: true,
      );
      return true;
    } on ApiException catch (error) {
      state = state.copyWith(status: AuthStatus.needsLogin, error: error.message);
      return false;
    }
  }

  /// Completes sign-in with a `session` cookie captured from the OIDC WebView.
  Future<void> completeOidcSignIn(String sessionCookie) async {
    client.applySession(sessionCookie);
    await _storage.writeSessionCookie(sessionCookie);
    await client.ensureCsrfToken();
    state = state.copyWith(status: AuthStatus.authenticated, clearError: true);
  }

  Future<void> signOut() async {
    final previous = state;
    state = state.copyWith(status: AuthStatus.loading, clearError: true);
    await client.logout();
    await _storage.clearSessionCookie();
    state = AuthState(
      status: AuthStatus.needsLogin,
      serverUrl: previous.serverUrl,
      username: previous.username,
      oidcEnabled: previous.oidcEnabled,
    );
  }
}
