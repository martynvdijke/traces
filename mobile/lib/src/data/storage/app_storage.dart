import 'key_value_store.dart';

/// Typed accessors over [KeyValueStore] for everything the app persists.
class AppStorage {
  AppStorage(this._store);

  final KeyValueStore _store;

  static const String serverUrlKey = 'traces.server_url';
  static const String sessionCookieKey = 'traces.session_cookie';
  static const String usernameKey = 'traces.username';
  static const String oidcEnabledKey = 'traces.oidc_enabled';
  static const String demoEnabledKey = 'traces.demo_enabled';
  static const String demoUsernameKey = 'traces.demo_username';
  static const String demoPasswordKey = 'traces.demo_password';

  Future<String?> readServerUrl() => _store.read(serverUrlKey);

  Future<void> writeServerUrl(String url) => _store.write(serverUrlKey, url);

  Future<String?> readSessionCookie() => _store.read(sessionCookieKey);

  Future<void> writeSessionCookie(String cookie) =>
      _store.write(sessionCookieKey, cookie);

  Future<void> clearSessionCookie() => _store.delete(sessionCookieKey);

  Future<String?> readUsername() => _store.read(usernameKey);

  Future<void> writeUsername(String username) =>
      _store.write(usernameKey, username);

  Future<bool> readOidcEnabled() async =>
      (await _store.read(oidcEnabledKey)) == 'true';

  Future<void> writeOidcEnabled(bool enabled) =>
      _store.write(oidcEnabledKey, enabled ? 'true' : 'false');

  Future<bool> readDemoEnabled() async =>
      (await _store.read(demoEnabledKey)) == 'true';

  Future<void> writeDemoEnabled(bool enabled) =>
      _store.write(demoEnabledKey, enabled ? 'true' : 'false');

  Future<String?> readDemoUsername() => _store.read(demoUsernameKey);

  Future<void> writeDemoUsername(String username) =>
      _store.write(demoUsernameKey, username);

  Future<String?> readDemoPassword() => _store.read(demoPasswordKey);

  Future<void> writeDemoPassword(String password) =>
      _store.write(demoPasswordKey, password);

  /// Replaces the stored demo credentials for the connected server. Disabled
  /// servers keep empty values so a previous server's demo login cannot leak
  /// into the next one.
  Future<void> writeDemoConfig({
    required bool enabled,
    required String username,
    required String password,
  }) async {
    await writeDemoEnabled(enabled);
    await writeDemoUsername(enabled ? username : '');
    await writeDemoPassword(enabled ? password : '');
  }
}
