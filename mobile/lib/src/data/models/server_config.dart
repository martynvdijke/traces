import 'json_utils.dart';

/// Public server configuration from `GET /api/config`.
class ServerConfig {
  const ServerConfig({
    this.oidcEnabled = false,
    this.demoEnabled = false,
    this.demoUsername = '',
    this.demoPassword = '',
  });

  final bool oidcEnabled;

  /// Whether this server offers the shared reviewer demo account.
  final bool demoEnabled;
  final String demoUsername;
  final String demoPassword;

  factory ServerConfig.fromJson(Map<String, dynamic> json) => ServerConfig(
    oidcEnabled: asBool(json['oidc_enabled']),
    demoEnabled: asBool(json['demo_enabled']),
    demoUsername: asString(json['demo_username']),
    demoPassword: asString(json['demo_password']),
  );
}
