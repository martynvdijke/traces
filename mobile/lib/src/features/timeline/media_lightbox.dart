import 'package:flutter/material.dart';

import '../../data/api/server_url.dart';
import '../../data/models/timeline_event.dart';

/// Full-screen, swipeable media lightbox for a list of timeline events.
class MediaLightbox extends StatefulWidget {
  const MediaLightbox({
    super.key,
    required this.items,
    required this.initialIndex,
    required this.baseUrl,
  });

  final List<TimelineEvent> items;
  final int initialIndex;
  final String baseUrl;

  /// Opens the lightbox as a full-screen route.
  static Future<void> show(
    BuildContext context, {
    required List<TimelineEvent> items,
    required int initialIndex,
    required String baseUrl,
  }) {
    if (items.isEmpty) return Future.value();
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        fullscreenDialog: true,
        builder: (_) => MediaLightbox(
          items: items,
          initialIndex: initialIndex.clamp(0, items.length - 1),
          baseUrl: baseUrl,
        ),
      ),
    );
  }

  @override
  State<MediaLightbox> createState() => _MediaLightboxState();
}

class _MediaLightboxState extends State<MediaLightbox> {
  late final PageController _controller;
  late int _index;

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex;
    _controller = PageController(initialPage: _index);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.items[_index];
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        title: Text('${_index + 1} / ${widget.items.length}'),
        leading: IconButton(
          icon: const Icon(Icons.close),
          tooltip: 'Close',
          onPressed: () => Navigator.of(context).pop(),
        ),
      ),
      body: Column(
        children: [
          Expanded(
            child: PageView.builder(
              controller: _controller,
              itemCount: widget.items.length,
              onPageChanged: (index) => setState(() => _index = index),
              itemBuilder: (context, index) => _MediaPane(
                event: widget.items[index],
                baseUrl: widget.baseUrl,
              ),
            ),
          ),
          _Caption(item: item),
        ],
      ),
    );
  }
}

class _MediaPane extends StatelessWidget {
  const _MediaPane({required this.event, required this.baseUrl});

  final TimelineEvent event;
  final String baseUrl;

  bool get _isImage =>
      event.mediaType == 'image' ||
      event.mediaType == 'boardgame' ||
      event.mediaType.isEmpty;

  @override
  Widget build(BuildContext context) {
    if (!_isImage) {
      final icon = event.mediaType == 'video'
          ? Icons.movie_outlined
          : Icons.audiotrack_outlined;
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 72, color: Colors.white70),
            const SizedBox(height: 12),
            Text(
              '${event.mediaType.isEmpty ? 'Media' : event.mediaType} attachment',
              style: const TextStyle(color: Colors.white70),
            ),
          ],
        ),
      );
    }

    final url = absoluteUrl(baseUrl, event.mediaUrl.isNotEmpty
        ? event.mediaUrl
        : event.previewUrl);
    return InteractiveViewer(
      maxScale: 5,
      child: Center(
        child: Image.network(
          url,
          fit: BoxFit.contain,
          loadingBuilder: (context, child, progress) {
            if (progress == null) return child;
            return const Center(
              child: CircularProgressIndicator(color: Colors.white),
            );
          },
          errorBuilder: (context, error, stack) => const Center(
            child: Icon(Icons.broken_image_outlined,
                size: 64, color: Colors.white38),
          ),
        ),
      ),
    );
  }
}

class _Caption extends StatelessWidget {
  const _Caption({required this.item});

  final TimelineEvent item;

  @override
  Widget build(BuildContext context) {
    final lines = <String>[
      if (item.date.isNotEmpty) item.date,
      if (item.location.isNotEmpty) item.location,
      if (item.tagList.isNotEmpty) item.tagList.map((t) => '#$t').join('  '),
    ];
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              item.title,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 18,
                fontWeight: FontWeight.w600,
              ),
            ),
            if (item.description.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(
                item.description,
                style: const TextStyle(color: Colors.white70),
              ),
            ],
            if (lines.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                lines.join('  ·  '),
                style: const TextStyle(color: Colors.white54, fontSize: 12),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
