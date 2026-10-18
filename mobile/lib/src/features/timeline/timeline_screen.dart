import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/models/timeline_event.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';
import '../person/person_profile_screen.dart';
import 'event_card.dart';
import 'media_lightbox.dart';
import 'timeline_filter_bar.dart';

/// Year timeline: the filtered list of memories for the selected year.
class TimelineScreen extends ConsumerWidget {
  const TimelineScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final asyncEvents = ref.watch(timelineProvider);
    final baseUrl = ref.watch(authControllerProvider).serverUrl;
    final filter = ref.watch(timelineFilterProvider);
    final mediaItems = asyncEvents.value
            ?.where((event) => event.hasMedia)
            .toList(growable: false) ??
        const <TimelineEvent>[];

    return Column(
      children: [
        const TimelineFilterBar(),
        Expanded(
          child: RefreshIndicator(
            onRefresh: () async {
              ref.invalidate(timelineProvider);
              await ref.read(timelineProvider.future);
            },
            child: AsyncValueView<List<TimelineEvent>>(
              value: asyncEvents,
              onRetry: () => ref.refresh(timelineProvider.future),
              isEmpty: (events) => events.isEmpty,
              empty: EmptyView(
                message: hasActiveFilters(filter)
                    ? 'No memories match these filters'
                    : 'No memories yet — capture one!',
              ),
              builder: (context, events) => ListView.builder(
                physics: const AlwaysScrollableScrollPhysics(),
                padding: const EdgeInsets.only(bottom: 96),
                itemCount: events.length,
                itemBuilder: (context, index) {
                  final event = events[index];
                  return EventCard(
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
                    onPersonTap: (person) => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) =>
                            PersonProfileScreen(personId: person.id),
                      ),
                    ),
                  );
                },
              ),
            ),
          ),
        ),
      ],
    );
  }
}
