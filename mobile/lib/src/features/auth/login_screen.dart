import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/providers.dart';
import 'oidc_webview_screen.dart';

/// Username/password sign-in, first-run admin creation, and SSO entry point.
class LoginScreen extends ConsumerStatefulWidget {
  const LoginScreen({super.key});

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen> {
  late final TextEditingController _username;
  late final TextEditingController _password;
  bool _obscure = true;

  @override
  void initState() {
    super.initState();
    _username = TextEditingController(
      text: ref.read(authControllerProvider).username,
    );
    _password = TextEditingController();
  }

  @override
  void dispose() {
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _signIn() async {
    FocusScope.of(context).unfocus();
    await ref.read(authControllerProvider.notifier).signIn(
          username: _username.text,
          password: _password.text,
        );
  }

  Future<void> _signInWithOidc() async {
    final cookie = await Navigator.of(context).push<String>(
      MaterialPageRoute(builder: (_) => const OidcWebViewScreen()),
    );
    if (!mounted || cookie == null || cookie.isEmpty) return;
    await ref
        .read(authControllerProvider.notifier)
        .completeOidcSignIn(cookie);
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authControllerProvider);
    final scheme = Theme.of(context).colorScheme;
    final isSetup = auth.setupRequired;

    return Scaffold(
      appBar: AppBar(
        title: Text(isSetup ? 'Create admin account' : 'Sign in'),
        actions: [
          IconButton(
            tooltip: 'Change server',
            icon: const Icon(Icons.dns_outlined),
            onPressed: auth.isBusy
                ? null
                : () =>
                    ref.read(authControllerProvider.notifier).forgetServer(),
          ),
        ],
      ),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  if (isSetup)
                    Card(
                      child: Padding(
                        padding: const EdgeInsets.all(12),
                        child: Text(
                          'This server has no users yet. Enter a username and '
                          'password (min. 8 characters) to create the admin account.',
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ),
                    ),
                  if (isSetup) const SizedBox(height: 16),
                  TextField(
                    key: const Key('login-username'),
                    controller: _username,
                    autocorrect: false,
                    textInputAction: TextInputAction.next,
                    decoration: const InputDecoration(
                      labelText: 'Username',
                      prefixIcon: Icon(Icons.person_outline),
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    key: const Key('login-password'),
                    controller: _password,
                    obscureText: _obscure,
                    onSubmitted: (_) => _signIn(),
                    decoration: InputDecoration(
                      labelText: 'Password',
                      prefixIcon: const Icon(Icons.lock_outline),
                      border: const OutlineInputBorder(),
                      suffixIcon: IconButton(
                        icon: Icon(
                          _obscure ? Icons.visibility_off : Icons.visibility,
                        ),
                        onPressed: () => setState(() => _obscure = !_obscure),
                      ),
                    ),
                  ),
                  if (auth.hasError) ...[
                    const SizedBox(height: 12),
                    Text(auth.error!, style: TextStyle(color: scheme.error)),
                  ],
                  const SizedBox(height: 20),
                  FilledButton.icon(
                    key: const Key('login-submit'),
                    onPressed: auth.isBusy ? null : _signIn,
                    icon: auth.isBusy
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.login),
                    label: Text(
                      isSetup
                          ? 'Create account & sign in'
                          : (auth.isBusy ? 'Signing in…' : 'Sign in'),
                    ),
                  ),
                  if (auth.oidcEnabled) ...[
                    const SizedBox(height: 12),
                    OutlinedButton.icon(
                      key: const Key('login-oidc'),
                      onPressed: auth.isBusy ? null : _signInWithOidc,
                      icon: const Icon(Icons.account_circle_outlined),
                      label: const Text('Sign in with SSO'),
                    ),
                  ],
                  const SizedBox(height: 24),
                  Text(
                    auth.serverUrl,
                    textAlign: TextAlign.center,
                    style: Theme.of(context)
                        .textTheme
                        .bodySmall
                        ?.copyWith(color: scheme.outline),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
