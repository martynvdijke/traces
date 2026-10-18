import 'key_value_store.dart';

/// Typed accessors over [KeyValueStore] for everything the app persists.
class AppStorage {
  AppStorage(this._store);

  final KeyValueStore _store;

  static const String serverUrlKey = 'traces.server_url';
  static const String sessionCookieKey = 'traces.session_cookie';
  static const String usernameKey = 'traces.username';
  static const String oidcEnabledKey = 'traces.oidc_enabled';

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
}
