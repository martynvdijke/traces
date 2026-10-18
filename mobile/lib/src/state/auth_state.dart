/// Where the app is in the onboarding / authentication flow.
enum AuthStatus {
  /// Restoring the persisted session (initial state).
  loading,

  /// No server configured yet.
  needsServer,

  /// Server reachable but no valid session.
  needsLogin,

  /// Session is valid.
  authenticated,
}

/// Immutable snapshot of the authentication state.
class AuthState {
  const AuthState({
    required this.status,
    this.serverUrl = '',
    this.username = '',
    this.error,
    this.oidcEnabled = false,
    this.setupRequired = false,
    this.connecting = false,
  });

  final AuthStatus status;
  final String serverUrl;
  final String username;
  final String? error;
  final bool oidcEnabled;
  final bool setupRequired;

  /// True while the very first server handshake is in flight (shown as a
  /// splash instead of the login form).
  final bool connecting;

  bool get isBusy => status == AuthStatus.loading;

  bool get hasError => error != null && error!.isNotEmpty;

  AuthState copyWith({
    AuthStatus? status,
    String? serverUrl,
    String? username,
    String? error,
    bool clearError = false,
    bool? oidcEnabled,
    bool? setupRequired,
    bool? connecting,
  }) =>
      AuthState(
        status: status ?? this.status,
        serverUrl: serverUrl ?? this.serverUrl,
        username: username ?? this.username,
        error: clearError ? null : (error ?? this.error),
        oidcEnabled: oidcEnabled ?? this.oidcEnabled,
        setupRequired: setupRequired ?? this.setupRequired,
        connecting: connecting ?? this.connecting,
      );
}
