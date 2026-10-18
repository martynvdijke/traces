import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';

import '../../data/models/event_stats.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';

/// GitHub-style 365-day activity heatmap plus a yearly stats summary.
class HeatmapScreen extends ConsumerWidget {
  const HeatmapScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final year = ref.watch(selectedYearProvider);
    final asyncContributions = ref.watch(contributionsProvider);
    final stats = ref.watch(yearStatsProvider).value;

    return AsyncValueView<Map<String, int>>(
      value: asyncContributions,
      onRetry: () => ref.refresh(contributionsProvider.future),
      builder: (context, contributions) => ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
        children: [
          _HeatmapCard(year: year, contributions: contributions),
          const SizedBox(height: 16),
          if (stats != null) _StatsCard(year: year, stats: stats),
        ],
      ),
    );
  }
}

class _HeatmapCard extends StatelessWidget {
  const _HeatmapCard({required this.year, required this.contributions});

  final int year;
  final Map<String, int> contributions;

  static const double _cell = 13;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final format = DateFormat('yyyy-MM-dd');

    final first = DateTime(year, 1, 1);
    final totalDays = DateTime(year, 12, 31).difference(first).inDays + 1;
    final leading = first.weekday - 1; // Monday-first grid
    final weeks = ((leading + totalDays) / 7).ceil();
    final maxCount = contributions.values
        .fold<int>(0, (best, value) => value > best ? value : best);
    final activeDays = contributions.values.where((v) => v > 0).length;
    final total = contributions.values.fold<int>(0, (sum, value) => sum + value);

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Activity in $year', style: theme.textTheme.titleMedium),
            const SizedBox(height: 2),
            Text(
              '$total memories across $activeDays days',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 14),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (var week = 0; week < weeks; week++)
                    Padding(
                      padding: const EdgeInsets.only(right: 2),
                      child: Column(
                        children: [
                          for (var weekday = 0; weekday < 7; weekday++)
                            _cellFor(
                              context: context,
                              index: week * 7 + weekday,
                              leading: leading,
                              totalDays: totalDays,
                              first: first,
                              format: format,
                              maxCount: maxCount,
                            ),
                        ],
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Text('Less', style: theme.textTheme.labelSmall),
                const SizedBox(width: 6),
                for (final step in const [0.0, 0.25, 0.5, 0.75, 1.0]) ...[
                  Container(
                    width: _cell,
                    height: _cell,
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(3),
                      color: step == 0
                          ? scheme.surfaceContainerHighest
                          : scheme.primary.withValues(alpha: 0.25 + 0.75 * step),
                    ),
                  ),
                  const SizedBox(width: 4),
                ],
                const SizedBox(width: 2),
                Text('More', style: theme.textTheme.labelSmall),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _cellFor({
    required BuildContext context,
    required int index,
    required int leading,
    required int totalDays,
    required DateTime first,
    required DateFormat format,
    required int maxCount,
  }) {
    final dayIndex = index - leading;
    if (dayIndex < 0 || dayIndex >= totalDays) {
      return const SizedBox(width: _cell, height: _cell + 2);
    }
    final scheme = Theme.of(context).colorScheme;
    final date = first.add(Duration(days: dayIndex));
    final key = format.format(date);
    final count = contributions[key] ?? 0;
    final intensity =
        count == 0 || maxCount == 0 ? 0.0 : count / maxCount;
    return Tooltip(
      message: '$key: $count ${count == 1 ? 'memory' : 'memories'}',
      child: Container(
        width: _cell,
        height: _cell,
        margin: const EdgeInsets.only(bottom: 2),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(3),
          color: count == 0
              ? scheme.surfaceContainerHighest
              : scheme.primary.withValues(alpha: 0.25 + 0.75 * intensity),
        ),
      ),
    );
  }
}

class _StatsCard extends StatelessWidget {
  const _StatsCard({required this.year, required this.stats});

  final int year;
  final EventStats stats;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final number = NumberFormat.decimalPattern();
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('$year at a glance', style: theme.textTheme.titleMedium),
            const SizedBox(height: 12),
            _StatRow(
              icon: Icons.timeline,
              label: 'Memories',
              value: number.format(stats.total),
            ),
            _StatRow(
              icon: Icons.photo_library_outlined,
              label: 'With media',
              value: number.format(stats.withMedia),
            ),
            _StatRow(
              icon: Icons.place_outlined,
              label: 'With coordinates',
              value: number.format(stats.withGeo),
            ),
            _StatRow(
              icon: Icons.person_outline,
              label: 'People',
              value: number.format(stats.personCount),
            ),
            _StatRow(
              icon: Icons.calendar_month_outlined,
              label: 'Years of history',
              value: number.format(stats.totalYears),
            ),
          ],
        ),
      ),
    );
  }
}

class _StatRow extends StatelessWidget {
  const _StatRow({
    required this.icon,
    required this.label,
    required this.value,
  });

  final IconData icon;
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Icon(icon, size: 18, color: theme.colorScheme.outline),
          const SizedBox(width: 12),
          Expanded(child: Text(label, style: theme.textTheme.bodyMedium)),
          Text(
            value,
            style: theme.textTheme.titleSmall
                ?.copyWith(fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}
