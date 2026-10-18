import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../state/providers.dart';

/// Shared year picker used by the timeline, map, heatmap, and comparison tabs.
class YearSelector extends ConsumerWidget {
  const YearSelector({super.key, this.firstYear = 2000});

  final int firstYear;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selected = ref.watch(selectedYearProvider);
    final currentYear = DateTime.now().year;
    final earliest = selected < firstYear ? selected : firstYear;
    final years = [
      for (var year = currentYear; year >= earliest; year--) year,
    ];

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8),
      child: DropdownButtonHideUnderline(
        child: DropdownButton<int>(
          value: selected,
          borderRadius: BorderRadius.circular(12),
          items: [
            for (final year in years)
              DropdownMenuItem<int>(value: year, child: Text('$year')),
          ],
          onChanged: (year) {
            if (year != null) {
              ref.read(selectedYearProvider.notifier).select(year);
            }
          },
        ),
      ),
    );
  }
}
