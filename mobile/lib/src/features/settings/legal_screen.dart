import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../../data/api/server_url.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';

/// Shows one of the server's public HTML pages (privacy policy, terms,
/// account deletion) inside the app.
///
/// JavaScript stays disabled: these pages are plain documents and need no
/// scripting, so there is no reason to enable it.
class LegalScreen extends ConsumerStatefulWidget {
  const LegalScreen({super.key, required this.title, required this.path});

  final String title;

  /// Server-relative path, e.g. `/privacy`.
  final String path;

  @override
  ConsumerState<LegalScreen> createState() => _LegalScreenState();
}

class _LegalScreenState extends ConsumerState<LegalScreen> {
  late final WebViewController _controller;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    final baseUrl = ref.read(authControllerProvider).serverUrl;
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.disabled)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageStarted: (_) {
            if (!mounted) return;
            setState(() {
              _loading = true;
              _error = null;
            });
          },
          onPageFinished: (_) {
            if (mounted) setState(() => _loading = false);
          },
          onWebResourceError: (error) {
            // Sub-resource failures (a missing icon, say) should not replace
            // the whole page.
            if (error.isForMainFrame == false) return;
            if (!mounted) return;
            setState(() {
              _loading = false;
              _error = error.description;
            });
          },
        ),
      )
      ..loadRequest(Uri.parse(absoluteUrl(baseUrl, widget.path)));
  }

  Future<void> _reload() async {
    setState(() {
      _error = null;
      _loading = true;
    });
    await _controller.loadRequest(
      Uri.parse(
        absoluteUrl(ref.read(authControllerProvider).serverUrl, widget.path),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final error = _error;
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: Stack(
        children: [
          WebViewWidget(controller: _controller),
          if (error != null)
            Positioned.fill(
              child: ColoredBox(
                color: Theme.of(context).colorScheme.surface,
                child: ErrorRetryView(
                  message: 'Could not load this page.\n$error',
                  onRetry: _reload,
                ),
              ),
            ),
          if (_loading) const LinearProgressIndicator(),
        ],
      ),
    );
  }
}
