import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/api/server_url.dart';
import '../../data/models/person.dart';
import '../../data/models/timeline_event.dart';
import '../../state/providers.dart';

/// A single timeline memory, with media thumbnail and quick actions.
class EventCard extends ConsumerWidget {
  const EventCard({
    super.key,
    required this.event,
    this.onOpenMedia,
    this.onPersonTap,
  });

  final TimelineEvent event;
  final VoidCallback? onOpenMedia;
  final ValueChanged<Person>? onPersonTap;

  bool get _isImage =>
      event.mediaType == 'image' ||
      event.mediaType == 'boardgame' ||
      event.mediaType.isEmpty;

  Future<void> _toggleFavorite(WidgetRef ref) async {
    await ref.read(eventsRepositoryProvider).toggleFavorite(event.id);
    ref.invalidate(timelineProvider);
    final personId = event.personId;
    if (personId != null) {
      ref.invalidate(personEventsProvider(personId));
    }
  }

  Future<void> _delete(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete memory?'),
        content: Text('“${event.title}” will be moved to the trash.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await ref.read(eventsRepositoryProvider).deleteEvent(event.id);
    ref.invalidate(timelineProvider);
    ref.invalidate(mapDataProvider);
    ref.invalidate(contributionsProvider);
    final personId = event.personId;
    if (personId != null) {
      ref.invalidate(personEventsProvider(personId));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final baseUrl = ref.watch(authControllerProvider).serverUrl;
    final tags = event.tagList;
    final person = event.person;

    return Card(
      clipBehavior: Clip.antiAlias,
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: InkWell(
        onTap: event.hasMedia ? onOpenMedia : null,
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (event.hasMedia) ...[
                _Thumbnail(
                  imageUrl: _isImage
                      ? absoluteUrl(baseUrl, event.previewUrl)
                      : null,
                  icon: event.mediaType == 'video'
                      ? Icons.play_circle_outline
                      : Icons.audiotrack_outlined,
                ),
                const SizedBox(width: 12),
              ],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        if (event.date.isNotEmpty) ...[
                          Icon(Icons.event, size: 14, color: scheme.outline),
                          const SizedBox(width: 4),
                          Text(
                            event.date,
                            style: theme.textTheme.labelSmall
                                ?.copyWith(color: scheme.outline),
                          ),
                        ],
                        const Spacer(),
                        if (event.isFavorite)
                          Icon(Icons.favorite,
                              size: 16, color: scheme.error),
                        _Menu(
                          onToggleFavorite: () => _toggleFavorite(ref),
                          onDelete: () => _delete(context, ref),
                        ),
                      ],
                    ),
                    Text(
                      event.title,
                      style: theme.textTheme.titleMedium
                          ?.copyWith(fontWeight: FontWeight.w600),
                    ),
                    if (event.description.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        event.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall,
                      ),
                    ],
                    if (event.location.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Row(
                        children: [
                          Icon(Icons.place_outlined,
                              size: 14, color: scheme.outline),
                          const SizedBox(width: 4),
                          Expanded(
                            child: Text(
                              event.location,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: theme.textTheme.labelSmall
                                  ?.copyWith(color: scheme.outline),
                            ),
                          ),
                        ],
                      ),
                    ],
                    if (person != null || tags.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 6,
                        runSpacing: 4,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          if (person != null)
                            _PersonChip(
                              person: person,
                              baseUrl: baseUrl,
                              onTap: onPersonTap == null
                                  ? null
                                  : () => onPersonTap!(person),
                            ),
                          for (final tag in tags.take(4)) _TagChip(label: tag),
                          if (tags.length > 4)
                            Text(
                              '+${tags.length - 4}',
                              style: theme.textTheme.labelSmall
                                  ?.copyWith(color: scheme.outline),
                            ),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Thumbnail extends StatelessWidget {
  const _Thumbnail({required this.imageUrl, required this.icon});

  final String? imageUrl;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return ClipRRect(
      borderRadius: BorderRadius.circular(10),
      child: SizedBox(
        width: 68,
        height: 68,
        child: imageUrl == null
            ? Container(
                color: scheme.surfaceContainerHighest,
                child: Icon(icon, color: scheme.onSurfaceVariant),
              )
            : Image.network(
                imageUrl!,
                fit: BoxFit.cover,
                errorBuilder: (context, error, stack) => Container(
                  color: scheme.surfaceContainerHighest,
                  child: Icon(Icons.image_not_supported_outlined,
                      color: scheme.onSurfaceVariant),
                ),
              ),
      ),
    );
  }
}

class _Menu extends StatelessWidget {
  const _Menu({required this.onToggleFavorite, required this.onDelete});

  final VoidCallback onToggleFavorite;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      icon: const Icon(Icons.more_vert, size: 18),
      padding: EdgeInsets.zero,
      iconSize: 18,
      onSelected: (value) {
        if (value == 'favorite') onToggleFavorite();
        if (value == 'delete') onDelete();
      },
      itemBuilder: (context) => const [
        PopupMenuItem(value: 'favorite', child: Text('Toggle favourite')),
        PopupMenuItem(value: 'delete', child: Text('Delete')),
      ],
    );
  }
}

class _PersonChip extends StatelessWidget {
  const _PersonChip({required this.person, required this.baseUrl, this.onTap});

  final Person person;
  final String baseUrl;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final avatar = person.avatarUrl.isEmpty
        ? null
        : absoluteUrl(baseUrl, person.avatarUrl);
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(20),
      child: Container(
        padding: const EdgeInsets.fromLTRB(2, 2, 8, 2),
        decoration: BoxDecoration(
          color: scheme.tertiaryContainer,
          borderRadius: BorderRadius.circular(20),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircleAvatar(
              radius: 9,
              backgroundColor: scheme.tertiary,
              backgroundImage: avatar == null ? null : NetworkImage(avatar),
              child: avatar == null
                  ? Text(
                      person.initial,
                      style: TextStyle(
                        fontSize: 9,
                        color: scheme.onTertiary,
                      ),
                    )
                  : null,
            ),
            const SizedBox(width: 5),
            Text(
              person.name,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: scheme.onTertiaryContainer,
                  ),
            ),
          ],
        ),
      ),
    );
  }
}

class _TagChip extends StatelessWidget {
  const _TagChip({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: scheme.secondaryContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
              color: scheme.onSecondaryContainer,
            ),
      ),
    );
  }
}
