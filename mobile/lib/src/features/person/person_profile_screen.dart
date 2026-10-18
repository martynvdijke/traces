import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/api/server_url.dart';
import '../../data/models/person.dart';
import '../../data/models/person_events.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';
import '../timeline/event_card.dart';
import '../timeline/media_lightbox.dart';

/// A person's profile: their details and every memory they appear in.
class PersonProfileScreen extends ConsumerWidget {
  const PersonProfileScreen({super.key, required this.personId});

  final int personId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final asyncData = ref.watch(personEventsProvider(personId));
    final baseUrl = ref.watch(authControllerProvider).serverUrl;

    return Scaffold(
      appBar: AppBar(title: Text(asyncData.value?.person.name ?? 'Person')),
      body: AsyncValueView<PersonEvents>(
        value: asyncData,
        onRetry: () => ref.refresh(personEventsProvider(personId).future),
        builder: (context, data) {
          final mediaItems = data.events
              .where((event) => event.hasMedia)
              .toList(growable: false);
          return ListView(
            padding: const EdgeInsets.only(bottom: 32),
            children: [
              _Header(person: data.person, baseUrl: baseUrl),
              if (data.events.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 48),
                  child: EmptyView(
                    message: 'No memories tagged with this person yet',
                  ),
                ),
              for (final event in data.events)
                EventCard(
                  event: event,
                  onOpenMedia: event.hasMedia
                      ? () => MediaLightbox.show(
                            context,
                            items: mediaItems,
                            initialIndex: mediaItems
                                .indexWhere((item) => item.id == event.id),
                            baseUrl: baseUrl,
                          )
                      : null,
                ),
            ],
          );
        },
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.person, required this.baseUrl});

  final Person person;
  final String baseUrl;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final avatar =
        person.avatarUrl.isEmpty ? null : absoluteUrl(baseUrl, person.avatarUrl);

    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        children: [
          CircleAvatar(
            radius: 40,
            backgroundColor: scheme.primaryContainer,
            backgroundImage: avatar == null ? null : NetworkImage(avatar),
            child: avatar == null
                ? Text(
                    person.initial,
                    style: theme.textTheme.headlineMedium?.copyWith(
                      color: scheme.onPrimaryContainer,
                    ),
                  )
                : null,
          ),
          const SizedBox(height: 12),
          Text(
            person.name,
            style: theme.textTheme.headlineSmall,
            textAlign: TextAlign.center,
          ),
          if (person.birthDate.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text('Born ${person.birthDate}', style: theme.textTheme.bodySmall),
          ],
          if (person.bio.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(person.bio, textAlign: TextAlign.center),
          ],
        ],
      ),
    );
  }
}
