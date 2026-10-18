import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/api/api_client.dart';
import '../data/models/event_filter.dart';
import '../data/models/event_stats.dart';
import '../data/models/map_data.dart';
import '../data/models/person.dart';
import '../data/models/person_events.dart';
import '../data/models/tag_count.dart';
import '../data/models/timeline_event.dart';
import '../data/models/wrapped_summary.dart';
import '../data/repositories/events_repository.dart';
import '../data/repositories/media_repository.dart';
import '../data/repositories/persons_repository.dart';
import '../data/repositories/stats_repository.dart';
import 'auth_controller.dart';
import 'auth_state.dart';

export 'auth_controller.dart';
export 'auth_state.dart';
export 'storage_providers.dart';

// --- Auth & transport -------------------------------------------------------

final authControllerProvider =
    NotifierProvider<AuthController, AuthState>(AuthController.new);

/// The single [ApiClient] owned by [AuthController]. It is stable for the app
/// lifetime; the session cookie is mutated in place on login/logout.
final apiClientProvider = Provider<ApiClient>(
  (ref) => ref.watch(authControllerProvider.notifier).client,
);

final eventsRepositoryProvider = Provider<EventsRepository>(
  (ref) => EventsRepository(ref.watch(apiClientProvider)),
);

final personsRepositoryProvider = Provider<PersonsRepository>(
  (ref) => PersonsRepository(ref.watch(apiClientProvider)),
);

final statsRepositoryProvider = Provider<StatsRepository>(
  (ref) => StatsRepository(ref.watch(apiClientProvider)),
);

final mediaRepositoryProvider = Provider<MediaRepository>(
  (ref) => MediaRepository(ref.watch(apiClientProvider)),
);

// --- Timeline year & filters ------------------------------------------------

/// The year shared by the timeline, map, and heatmap tabs.
class SelectedYearController extends Notifier<int> {
  @override
  int build() => DateTime.now().year;

  void select(int year) {
    if (year != state) state = year;
  }
}

final selectedYearProvider =
    NotifierProvider<SelectedYearController, int>(SelectedYearController.new);

class TimelineFilterController extends Notifier<EventFilter> {
  @override
  EventFilter build() => const EventFilter();

  void setQuery(String query) => state = state.copyWith(query: query);

  void setMonth(int? month) => state = month == null
      ? state.copyWith(clearMonth: true)
      : state.copyWith(month: month);

  void setTag(String? tag) => state = (tag == null || tag.isEmpty)
      ? state.copyWith(clearTag: true)
      : state.copyWith(tag: tag);

  void setPersonId(int? personId) => state = personId == null
      ? state.copyWith(clearPerson: true)
      : state.copyWith(personId: personId);

  void setMediaType(MediaCategory? category) =>
      state = (category == null || category == MediaCategory.other)
          ? state.copyWith(clearMediaType: true)
          : state.copyWith(mediaType: category.wireValue);

  void toggleFavoritesOnly() =>
      state = state.copyWith(favoritesOnly: !state.favoritesOnly);

  void clear() => state = const EventFilter();
}

final timelineFilterProvider =
    NotifierProvider<TimelineFilterController, EventFilter>(
  TimelineFilterController.new,
);

bool hasActiveFilters(EventFilter filter) =>
    filter.hasQuery ||
    filter.month != null ||
    (filter.tag?.isNotEmpty ?? false) ||
    filter.personId != null ||
    (filter.mediaType?.isNotEmpty ?? false) ||
    filter.favoritesOnly;

// --- Data providers ---------------------------------------------------------

final timelineProvider = FutureProvider<List<TimelineEvent>>((ref) {
  final year = ref.watch(selectedYearProvider);
  final filter = ref.watch(timelineFilterProvider).copyWith(year: year);
  return ref.watch(eventsRepositoryProvider).fetchTimeline(filter);
});

final mapDataProvider = FutureProvider<MapData>((ref) {
  final year = ref.watch(selectedYearProvider);
  return ref.watch(statsRepositoryProvider).fetchMapData(year);
});

final contributionsProvider = FutureProvider<Map<String, int>>((ref) {
  final year = ref.watch(selectedYearProvider);
  return ref.watch(statsRepositoryProvider).fetchContributions(year);
});

final tagsProvider = FutureProvider<List<TagCount>>((ref) {
  final year = ref.watch(selectedYearProvider);
  return ref.watch(statsRepositoryProvider).fetchTags(year);
});

final yearStatsProvider = FutureProvider<EventStats>((ref) {
  final year = ref.watch(selectedYearProvider);
  return ref.watch(statsRepositoryProvider).fetchStats(year);
});

final personsProvider = FutureProvider<List<Person>>(
  (ref) => ref.watch(personsRepositoryProvider).fetchPersons(),
);

final personEventsProvider = FutureProvider.family<PersonEvents, int>(
  (ref, personId) =>
      ref.watch(personsRepositoryProvider).fetchPersonEvents(personId),
);

// --- Multi-year comparison --------------------------------------------------

class CompareSelection {
  const CompareSelection({required this.leftYear, required this.rightYear});

  final int leftYear;
  final int rightYear;

  CompareSelection copyWith({int? leftYear, int? rightYear}) => CompareSelection(
        leftYear: leftYear ?? this.leftYear,
        rightYear: rightYear ?? this.rightYear,
      );
}

class CompareYearsController extends Notifier<CompareSelection> {
  @override
  CompareSelection build() {
    final year = DateTime.now().year;
    return CompareSelection(leftYear: year, rightYear: year - 1);
  }

  void setLeftYear(int year) => state = state.copyWith(leftYear: year);

  void setRightYear(int year) => state = state.copyWith(rightYear: year);
}

final compareYearsProvider =
    NotifierProvider<CompareYearsController, CompareSelection>(
  CompareYearsController.new,
);

/// Two [WrappedSummary]s: index 0 = left year, index 1 = right year.
final compareProvider = FutureProvider<List<WrappedSummary>>((ref) async {
  final selection = ref.watch(compareYearsProvider);
  final repository = ref.watch(statsRepositoryProvider);
  return Future.wait([
    repository.fetchWrapped(selection.leftYear),
    repository.fetchWrapped(selection.rightYear),
  ]);
});
