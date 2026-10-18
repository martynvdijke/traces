import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/providers.dart';
import '../../widgets/year_selector.dart';
import '../capture/capture_screen.dart';
import '../compare/compare_screen.dart';
import '../heatmap/heatmap_screen.dart';
import '../map/map_screen.dart';
import '../timeline/timeline_screen.dart';

/// Bottom-navigation shell hosting the five main tabs.
class HomeShell extends ConsumerStatefulWidget {
  const HomeShell({super.key});

  @override
  ConsumerState<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends ConsumerState<HomeShell> {
  int _index = 0;

  static const List<String> _titles = [
    'Timeline',
    'Map',
    'Log a memory',
    'Activity',
    'Compare',
  ];

  void _refreshAll() {
    ref.invalidate(timelineProvider);
    ref.invalidate(mapDataProvider);
    ref.invalidate(contributionsProvider);
    ref.invalidate(tagsProvider);
    ref.invalidate(yearStatsProvider);
    ref.invalidate(personsProvider);
    ref.invalidate(compareProvider);
    ref.invalidate(personEventsProvider);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_titles[_index]),
        actions: [
          if (_index != 2) const YearSelector(),
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _refreshAll,
          ),
          IconButton(
            tooltip: 'Sign out',
            icon: const Icon(Icons.logout),
            onPressed: () =>
                ref.read(authControllerProvider.notifier).signOut(),
          ),
        ],
      ),
      body: IndexedStack(
        index: _index,
        children: const [
          TimelineScreen(),
          MapScreen(),
          CaptureScreen(),
          HeatmapScreen(),
          CompareScreen(),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (index) => setState(() => _index = index),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.timeline_outlined),
            selectedIcon: Icon(Icons.timeline),
            label: 'Timeline',
          ),
          NavigationDestination(
            icon: Icon(Icons.map_outlined),
            selectedIcon: Icon(Icons.map),
            label: 'Map',
          ),
          NavigationDestination(
            icon: Icon(Icons.add_a_photo_outlined),
            selectedIcon: Icon(Icons.add_a_photo),
            label: 'Capture',
          ),
          NavigationDestination(
            icon: Icon(Icons.grid_on_outlined),
            selectedIcon: Icon(Icons.grid_on),
            label: 'Activity',
          ),
          NavigationDestination(
            icon: Icon(Icons.compare_arrows_outlined),
            selectedIcon: Icon(Icons.compare_arrows),
            label: 'Compare',
          ),
        ],
      ),
    );
  }
}
