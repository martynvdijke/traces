import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/api/api_exception.dart';
import '../../state/providers.dart';
import 'legal_screen.dart';

/// Account, privacy, and legal controls, including the data export and
/// account-deletion flows.
class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  bool _privacyLoading = true;
  bool? _analyticsOptOut;
  bool _privacySaving = false;
  String? _privacyError;

  @override
  void initState() {
    super.initState();
    _loadPrivacy();
  }

  Future<void> _loadPrivacy() async {
    setState(() {
      _privacyLoading = true;
      _privacyError = null;
    });
    try {
      final data = await ref.read(apiClientProvider).fetchPrivacySettings();
      if (!mounted) return;
      setState(() {
        _analyticsOptOut = data['analytics_opt_out'] == true;
        _privacyLoading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _privacyLoading = false;
        _privacyError = _messageFor(error);
      });
    }
  }

  Future<void> _setAnalytics(bool allowed) async {
    final previous = _analyticsOptOut ?? false;
    setState(() {
      _analyticsOptOut = !allowed;
      _privacySaving = true;
      _privacyError = null;
    });
    try {
      await ref
          .read(apiClientProvider)
          .updatePrivacySettings(analyticsOptOut: !allowed);
      if (!mounted) return;
      setState(() => _privacySaving = false);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _analyticsOptOut = previous;
        _privacySaving = false;
      });
      _showMessage(_messageFor(error));
    }
  }

  void _showMessage(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _signOut() async {
    final navigator = Navigator.of(context);
    await ref.read(authControllerProvider.notifier).signOut();
    navigator.popUntil((route) => route.isFirst);
  }

  void _openLegal(String title, String path) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => LegalScreen(title: title, path: path),
      ),
    );
  }

  void _openExport() {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (_) => const _ExportSheet(),
    );
  }

  void _confirmDeletion() {
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => const _DeleteAccountDialog(),
    );
  }

  String _privacySubtitle() {
    if (_privacyLoading) return 'Loading…';
    if (_privacyError != null) return _privacyError!;
    if (_privacySaving) return 'Saving…';
    if (_analyticsOptOut == true) {
      return 'Off. The web interface will not load analytics for your account.';
    }
    return 'On. The web interface may load anonymous analytics for your account.';
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authControllerProvider);
    final scheme = Theme.of(context).colorScheme;
    final privacyEnabled =
        !_privacyLoading &&
        !_privacySaving &&
        _privacyError == null &&
        _analyticsOptOut != null;

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: [
          const _SectionHeader('Account'),
          _SectionCard(
            children: [
              ListTile(
                key: const Key('settings-username'),
                leading: const Icon(Icons.person_outline),
                title: const Text('Username'),
                subtitle: Text(
                  auth.username.isEmpty ? 'Not signed in' : auth.username,
                ),
              ),
              ListTile(
                key: const Key('settings-server-url'),
                leading: const Icon(Icons.dns_outlined),
                title: const Text('Server'),
                subtitle: Text(
                  auth.serverUrl.isEmpty ? '—' : auth.serverUrl,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              ListTile(
                key: const Key('settings-sign-out'),
                leading: const Icon(Icons.logout),
                title: const Text('Sign out'),
                onTap: _signOut,
              ),
            ],
          ),
          const _SectionHeader('Data & privacy'),
          _SectionCard(
            children: [
              ListTile(
                key: const Key('settings-export'),
                leading: const Icon(Icons.download_outlined),
                title: const Text('Download my data'),
                subtitle: const Text(
                  'Your profile, memories, people, and preferences as JSON.',
                ),
                onTap: _openExport,
              ),
              SwitchListTile(
                key: const Key('settings-analytics'),
                secondary: const Icon(Icons.insights_outlined),
                title: const Text('Allow anonymous analytics'),
                subtitle: Text(_privacySubtitle()),
                value: _analyticsOptOut != null && !_analyticsOptOut!,
                onChanged: privacyEnabled ? _setAnalytics : null,
              ),
              if (_privacyError != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      key: const Key('settings-privacy-retry'),
                      onPressed: _loadPrivacy,
                      icon: const Icon(Icons.refresh),
                      label: const Text('Retry'),
                    ),
                  ),
                ),
              ListTile(
                key: const Key('settings-delete'),
                leading: Icon(
                  Icons.delete_forever_outlined,
                  color: scheme.error,
                ),
                title: Text(
                  'Delete my account',
                  style: TextStyle(color: scheme.error),
                ),
                subtitle: const Text(
                  'Permanently removes your account and memories.',
                ),
                onTap: _confirmDeletion,
              ),
            ],
          ),
          const _SectionHeader('Legal'),
          _SectionCard(
            children: [
              ListTile(
                key: const Key('settings-privacy-policy'),
                leading: const Icon(Icons.privacy_tip_outlined),
                title: const Text('Privacy policy'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _openLegal('Privacy policy', '/privacy'),
              ),
              ListTile(
                key: const Key('settings-terms'),
                leading: const Icon(Icons.description_outlined),
                title: const Text('Terms of use'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _openLegal('Terms of use', '/terms'),
              ),
              ListTile(
                key: const Key('settings-delete-page'),
                leading: const Icon(Icons.person_off_outlined),
                title: const Text('Delete your account'),
                subtitle: const Text(
                  'The public deletion page for this server.',
                ),
                trailing: const Icon(Icons.chevron_right),
                onTap: () =>
                    _openLegal('Delete your account', '/account/delete'),
              ),
            ],
          ),
          const _SectionHeader('About'),
          const _SectionCard(
            children: [
              ListTile(
                key: Key('settings-about'),
                leading: Icon(Icons.camera_roll_outlined),
                title: Text('TRACES'),
                subtitle: Text('Self-hosted memory log'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _SectionHeader extends StatelessWidget {
  const _SectionHeader(this.title);

  final String title;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 20, 4, 8),
      child: Text(
        title,
        style: theme.textTheme.titleSmall?.copyWith(
          color: theme.colorScheme.primary,
        ),
      ),
    );
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          for (var index = 0; index < children.length; index++) ...[
            if (index > 0) const Divider(height: 1),
            children[index],
          ],
        ],
      ),
    );
  }
}

/// Bottom sheet that builds the export, then offers it as JSON on the
/// clipboard. The document itself is never rendered — only its size.
class _ExportSheet extends ConsumerStatefulWidget {
  const _ExportSheet();

  @override
  ConsumerState<_ExportSheet> createState() => _ExportSheetState();
}

class _ExportSheetState extends ConsumerState<_ExportSheet> {
  late Future<Map<String, dynamic>> _export;

  @override
  void initState() {
    super.initState();
    _export = ref.read(apiClientProvider).exportAccount();
  }

  void _retry() {
    setState(() => _export = ref.read(apiClientProvider).exportAccount());
  }

  Future<void> _copy(String text) async {
    await Clipboard.setData(ClipboardData(text: text));
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(const SnackBar(content: Text('JSON copied')));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return SafeArea(
      child: SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 0, 24, 24),
          child: FutureBuilder<Map<String, dynamic>>(
            future: _export,
            builder: (context, snapshot) {
              if (snapshot.hasError) {
                return Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      'Could not prepare your data',
                      style: theme.textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      _messageFor(snapshot.error!),
                      style: TextStyle(color: scheme.error),
                    ),
                    const SizedBox(height: 16),
                    FilledButton.tonalIcon(
                      key: const Key('settings-export-retry'),
                      onPressed: _retry,
                      icon: const Icon(Icons.refresh),
                      label: const Text('Retry'),
                    ),
                    const SizedBox(height: 8),
                    TextButton(
                      onPressed: () => Navigator.of(context).pop(),
                      child: const Text('Close'),
                    ),
                  ],
                );
              }

              final data = snapshot.data;
              if (data == null) {
                return const Padding(
                  padding: EdgeInsets.symmetric(vertical: 24),
                  child: Row(
                    children: [
                      SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                      SizedBox(width: 16),
                      Text('Preparing your data…'),
                    ],
                  ),
                );
              }

              final text = jsonEncode(data);
              final size = _formatBytes(utf8.encode(text).length);
              return Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text('Your data', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Text(
                    'A $size JSON document is ready. Copy it somewhere safe, '
                    'then open it with any text editor.',
                    style: theme.textTheme.bodyMedium,
                  ),
                  const SizedBox(height: 20),
                  FilledButton.icon(
                    key: const Key('settings-export-copy'),
                    onPressed: () => _copy(text),
                    icon: const Icon(Icons.copy_all_outlined),
                    label: const Text('Copy JSON'),
                  ),
                  const SizedBox(height: 8),
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Close'),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}

/// Type-to-confirm dialog. It performs the deletion itself so progress and
/// errors stay visible while the request is in flight.
class _DeleteAccountDialog extends ConsumerStatefulWidget {
  const _DeleteAccountDialog();

  @override
  ConsumerState<_DeleteAccountDialog> createState() =>
      _DeleteAccountDialogState();
}

class _DeleteAccountDialogState extends ConsumerState<_DeleteAccountDialog> {
  static const String _confirmation = 'DELETE';

  final TextEditingController _controller = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  bool get _canDelete => _controller.text.trim() == _confirmation && !_busy;

  Future<void> _delete() async {
    final navigator = Navigator.of(context);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiClientProvider).deleteAccount();
      await ref.read(authControllerProvider.notifier).signOutAfterDeletion();
      if (!mounted) return;
      navigator.pop();
      navigator.popUntil((route) => route.isFirst);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = _deleteMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return AlertDialog(
      title: const Text('Delete your account?'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'This permanently deletes your profile, your memories, and the '
              'photos, videos, and audio attached to them, plus all signed-in '
              'sessions.',
            ),
            const SizedBox(height: 12),
            const Text(
              'Shared person records that other accounts reference may stay. '
              'Rotated backups may hold older copies until they expire.',
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('settings-delete-field'),
              controller: _controller,
              enabled: !_busy,
              autocorrect: false,
              textCapitalization: TextCapitalization.characters,
              onChanged: (_) => setState(() {}),
              decoration: const InputDecoration(
                labelText: 'Type DELETE to confirm',
                border: OutlineInputBorder(),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(_error!, style: TextStyle(color: scheme.error)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('settings-delete-confirm'),
          onPressed: _canDelete ? _delete : null,
          style: FilledButton.styleFrom(
            backgroundColor: scheme.error,
            foregroundColor: scheme.onError,
          ),
          child: _busy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('Delete'),
        ),
      ],
    );
  }
}

String _messageFor(Object error) =>
    error is ApiException ? error.message : 'Something went wrong. Try again.';

String _deleteMessage(Object error) {
  if (error is ApiException) {
    switch (error.statusCode) {
      case 403:
        return 'This is the server’s original admin account. It cannot be '
            'deleted from the app.';
      case 409:
        return 'This is the last administrator. Add another administrator '
            'before deleting this account.';
    }
    return error.message;
  }
  return 'Could not delete your account. Try again.';
}

String _formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  final kb = bytes / 1024;
  if (kb < 1024) return '${kb.toStringAsFixed(1)} KB';
  return '${(kb / 1024).toStringAsFixed(1)} MB';
}
