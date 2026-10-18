import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/models/event_filter.dart';
import '../../data/models/person.dart';
import '../../state/providers.dart';

const List<String> _monthNames = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

/// Search box plus filter chips (month, media type, tag, person, favourites).
class TimelineFilterBar extends ConsumerStatefulWidget {
  const TimelineFilterBar({super.key});

  @override
  ConsumerState<TimelineFilterBar> createState() => _TimelineFilterBarState();
}

class _TimelineFilterBarState extends ConsumerState<TimelineFilterBar> {
  late final TextEditingController _search;
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    _search = TextEditingController(
      text: ref.read(timelineFilterProvider).query,
    );
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    super.dispose();
  }

  void _onQueryChanged(String value) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 350), () {
      ref.read(timelineFilterProvider.notifier).setQuery(value);
    });
  }

  void _clearQuery() {
    _search.clear();
    ref.read(timelineFilterProvider.notifier).setQuery('');
  }

  @override
  Widget build(BuildContext context) {
    final filter = ref.watch(timelineFilterProvider);
    final controller = ref.read(timelineFilterProvider.notifier);
    final tags = ref.watch(tagsProvider).value ?? const [];
    final persons = ref.watch(personsProvider).value ?? const [];
    final sortedTags = [...tags]..sort((a, b) => b.count.compareTo(a.count));

    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          TextField(
            key: const Key('timeline-search'),
            controller: _search,
            textInputAction: TextInputAction.search,
            onChanged: _onQueryChanged,
            onSubmitted: (value) => controller.setQuery(value),
            decoration: InputDecoration(
              isDense: true,
              hintText: 'Search memories',
              prefixIcon: const Icon(Icons.search),
              suffixIcon: filter.hasQuery
                  ? IconButton(
                      icon: const Icon(Icons.close),
                      tooltip: 'Clear search',
                      onPressed: _clearQuery,
                    )
                  : null,
              border:
                  OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
            ),
          ),
          const SizedBox(height: 8),
          SizedBox(
            height: 36,
            child: ListView(
              scrollDirection: Axis.horizontal,
              children: [
                _MenuChip(
                  label: filter.month == null
                      ? 'Month'
                      : _monthNames[filter.month! - 1],
                  icon: Icons.calendar_month_outlined,
                  selected: filter.month != null,
                  options: [
                    const _Option('', 'All months'),
                    for (var month = 1; month <= 12; month++)
                      _Option(
                        month.toString().padLeft(2, '0'),
                        _monthNames[month - 1],
                      ),
                  ],
                  onSelected: (value) => controller.setMonth(
                    value.isEmpty ? null : int.parse(value),
                  ),
                ),
                const SizedBox(width: 8),
                _MenuChip(
                  label: MediaCategory.fromWire(filter.mediaType)?.label ??
                      'All types',
                  icon: Icons.perm_media_outlined,
                  selected: filter.mediaType != null,
                  options: [
                    const _Option('', 'All types'),
                    for (final category in MediaCategory.values)
                      if (category.wireValue.isNotEmpty)
                        _Option(category.wireValue, category.label),
                  ],
                  onSelected: (value) => controller
                      .setMediaType(MediaCategory.fromWire(value)),
                ),
                const SizedBox(width: 8),
                _MenuChip(
                  label: _personLabel(persons, filter.personId),
                  icon: Icons.person_outline,
                  selected: filter.personId != null,
                  options: [
                    const _Option('', 'Anyone'),
                    for (final person in persons)
                      _Option('${person.id}', person.name),
                  ],
                  onSelected: (value) => controller.setPersonId(
                    value.isEmpty ? null : int.parse(value),
                  ),
                ),
                const SizedBox(width: 8),
                FilterChip(
                  label: const Text('Favourites'),
                  avatar: Icon(
                    filter.favoritesOnly
                        ? Icons.favorite
                        : Icons.favorite_border,
                    size: 16,
                  ),
                  selected: filter.favoritesOnly,
                  onSelected: (_) => controller.toggleFavoritesOnly(),
                ),
                for (final tag in sortedTags.take(15)) ...[
                  const SizedBox(width: 8),
                  FilterChip(
                    label: Text('${tag.name} (${tag.count})'),
                    selected: filter.tag == tag.name,
                    onSelected: (selected) =>
                        controller.setTag(selected ? tag.name : null),
                  ),
                ],
                if (hasActiveFilters(filter)) ...[
                  const SizedBox(width: 8),
                  ActionChip(
                    avatar: const Icon(Icons.filter_alt_off, size: 16),
                    label: const Text('Clear'),
                    onPressed: controller.clear,
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Option {
  const _Option(this.value, this.label);

  final String value;
  final String label;
}

String _personLabel(List<Person> persons, int? personId) {
  if (personId == null) return 'Person';
  for (final person in persons) {
    if (person.id == personId) return person.name;
  }
  return 'Person';
}

class _MenuChip extends StatelessWidget {
  const _MenuChip({
    required this.label,
    required this.options,
    required this.onSelected,
    required this.selected,
    this.icon,
  });

  final String label;
  final List<_Option> options;
  final ValueChanged<String> onSelected;
  final bool selected;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return PopupMenuButton<String>(
      tooltip: label,
      onSelected: onSelected,
      itemBuilder: (context) => [
        for (final option in options)
          PopupMenuItem<String>(
            value: option.value,
            child: Text(option.label),
          ),
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12),
        decoration: BoxDecoration(
          color: selected ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(20),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (icon != null) ...[
              Icon(
                icon,
                size: 16,
                color: selected
                    ? scheme.onSecondaryContainer
                    : scheme.onSurfaceVariant,
              ),
              const SizedBox(width: 6),
            ],
            Text(
              label,
              style: Theme.of(context).textTheme.labelLarge?.copyWith(
                    color: selected
                        ? scheme.onSecondaryContainer
                        : scheme.onSurfaceVariant,
                  ),
            ),
            Icon(
              Icons.arrow_drop_down,
              size: 18,
              color: selected
                  ? scheme.onSecondaryContainer
                  : scheme.onSurfaceVariant,
            ),
          ],
        ),
      ),
    );
  }
}
