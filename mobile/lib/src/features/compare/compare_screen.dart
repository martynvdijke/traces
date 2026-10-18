import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/models/wrapped_summary.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';

/// Side-by-side comparison of two years using `/api/wrapped`.
class CompareScreen extends ConsumerWidget {
  const CompareScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selection = ref.watch(compareYearsProvider);
    final asyncCompare = ref.watch(compareProvider);
    final currentYear = DateTime.now().year;
    final years = [
      for (var year = currentYear; year >= currentYear - 20; year--) year,
    ];

    return AsyncValueView<List<WrappedSummary>>(
      value: asyncCompare,
      onRetry: () => ref.refresh(compareProvider.future),
      builder: (context, summaries) {
        final left = summaries[0];
        final right = summaries[1];
        return ListView(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
          children: [
            Row(
              children: [
                Expanded(
                  child: _YearPicker(
                    label: 'Year A',
                    year: selection.leftYear,
                    years: years,
                    onSelected: (year) => ref
                        .read(compareYearsProvider.notifier)
                        .setLeftYear(year),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: _YearPicker(
                    label: 'Year B',
                    year: selection.rightYear,
                    years: years,
                    onSelected: (year) => ref
                        .read(compareYearsProvider.notifier)
                        .setRightYear(year),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 20),
            _HeaderRow(leftYear: left.year, rightYear: right.year),
            _CompareRow(
              label: 'Memories',
              left: left.totalEvents,
              right: right.totalEvents,
            ),
            _CompareRow(
              label: 'Media uploaded',
              left: left.totalMedia,
              right: right.totalMedia,
            ),
            _CompareRow(
              label: 'Favourites',
              left: left.favoriteCount,
              right: right.favoriteCount,
            ),
            _CompareRow(
              label: 'Longest streak (days)',
              left: left.longestStreak,
              right: right.longestStreak,
            ),
            _CompareRow(
              label: 'Busiest month',
              left: left.busiestMonthCount,
              right: right.busiestMonthCount,
              leftNote: left.busiestMonth,
              rightNote: right.busiestMonth,
            ),
            const SizedBox(height: 20),
            _HighlightCard(
              title: 'Most memorable',
              leftNote: left.topEvent,
              leftDetail: left.topEventDate,
              rightNote: right.topEvent,
              rightDetail: right.topEventDate,
            ),
            const SizedBox(height: 12),
            _HighlightCard(
              title: 'Most tagged memory',
              leftNote: left.mostTagsTitle,
              leftDetail: '${left.mostTagsCount} tags',
              rightNote: right.mostTagsTitle,
              rightDetail: '${right.mostTagsCount} tags',
            ),
          ],
        );
      },
    );
  }
}

class _YearPicker extends StatelessWidget {
  const _YearPicker({
    required this.label,
    required this.year,
    required this.years,
    required this.onSelected,
  });

  final String label;
  final int year;
  final List<int> years;
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return PopupMenuButton<int>(
      onSelected: onSelected,
      itemBuilder: (context) => [
        for (final option in years)
          PopupMenuItem<int>(value: option, child: Text('$option')),
      ],
      child: InputDecorator(
        decoration: const InputDecoration(
          border: OutlineInputBorder(),
          isDense: true,
        ),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(label, style: theme.textTheme.labelSmall),
                Text('$year', style: theme.textTheme.titleMedium),
              ],
            ),
            const Icon(Icons.arrow_drop_down),
          ],
        ),
      ),
    );
  }
}

class _HeaderRow extends StatelessWidget {
  const _HeaderRow({required this.leftYear, required this.rightYear});

  final int leftYear;
  final int rightYear;

  @override
  Widget build(BuildContext context) {
    final style = Theme.of(context).textTheme.labelMedium;
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: Row(
        children: [
          const Expanded(flex: 4, child: SizedBox()),
          Expanded(
            flex: 2,
            child: Text('$leftYear', textAlign: TextAlign.end, style: style),
          ),
          Expanded(
            flex: 2,
            child: Text('$rightYear', textAlign: TextAlign.end, style: style),
          ),
          const Expanded(flex: 2, child: SizedBox()),
        ],
      ),
    );
  }
}

class _CompareRow extends StatelessWidget {
  const _CompareRow({
    required this.label,
    required this.left,
    required this.right,
    this.leftNote,
    this.rightNote,
  });

  final String label;
  final int left;
  final int right;
  final String? leftNote;
  final String? rightNote;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final style = theme.textTheme.titleSmall
        ?.copyWith(fontWeight: FontWeight.w600);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Expanded(
            flex: 4,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(label, style: theme.textTheme.bodyMedium),
                if ((leftNote?.isNotEmpty ?? false) ||
                    (rightNote?.isNotEmpty ?? false))
                  Text(
                    '${leftNote ?? '—'} · ${rightNote ?? '—'}',
                    style: theme.textTheme.labelSmall,
                  ),
              ],
            ),
          ),
          Expanded(
            flex: 2,
            child: Text('$left', textAlign: TextAlign.end, style: style),
          ),
          Expanded(
            flex: 2,
            child: Text('$right', textAlign: TextAlign.end, style: style),
          ),
          Expanded(
            flex: 2,
            child: Align(
              alignment: Alignment.centerRight,
              child: _Delta(delta: left - right),
            ),
          ),
        ],
      ),
    );
  }
}

class _Delta extends StatelessWidget {
  const _Delta({required this.delta});

  final int delta;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    if (delta == 0) {
      return Text('—', style: TextStyle(color: scheme.outline));
    }
    final up = delta > 0;
    final color = up ? scheme.tertiary : scheme.error;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        '${up ? '+' : ''}$delta',
        style: TextStyle(color: color, fontSize: 12),
      ),
    );
  }
}

class _HighlightCard extends StatelessWidget {
  const _HighlightCard({
    required this.title,
    required this.leftNote,
    required this.leftDetail,
    required this.rightNote,
    required this.rightDetail,
  });

  final String title;
  final String leftNote;
  final String leftDetail;
  final String rightNote;
  final String rightDetail;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: theme.textTheme.titleSmall),
            const SizedBox(height: 12),
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: _Highlight(
                    note: leftNote,
                    detail: leftDetail,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: _Highlight(
                    note: rightNote,
                    detail: rightDetail,
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _Highlight extends StatelessWidget {
  const _Highlight({required this.note, required this.detail});

  final String note;
  final String detail;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (note.isEmpty) {
      return Text('—', style: theme.textTheme.bodySmall);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(note, style: theme.textTheme.bodyMedium),
        Text(
          detail,
          style: theme.textTheme.labelSmall?.copyWith(
            color: theme.colorScheme.outline,
          ),
        ),
      ],
    );
  }
}
