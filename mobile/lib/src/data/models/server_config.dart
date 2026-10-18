import 'json_utils.dart';

/// Public server configuration from `GET /api/config`.
class ServerConfig {
  const ServerConfig({this.oidcEnabled = false});

  final bool oidcEnabled;

  factory ServerConfig.fromJson(Map<String, dynamic> json) =>
      ServerConfig(oidcEnabled: asBool(json['oidc_enabled']));
}
