import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';

import '../../data/api/server_url.dart';
import '../../data/models/map_data.dart';
import '../../state/providers.dart';
import '../../widgets/async_value_view.dart';

/// OpenStreetMap view of every geo-tagged memory in the selected year.
class MapScreen extends ConsumerWidget {
  const MapScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final asyncData = ref.watch(mapDataProvider);
    final baseUrl = ref.watch(authControllerProvider).serverUrl;
    final year = ref.watch(selectedYearProvider);

    return AsyncValueView<MapData>(
      value: asyncData,
      onRetry: () => ref.refresh(mapDataProvider.future),
      isEmpty: (data) => data.features.isEmpty,
      empty: EmptyView(message: 'No geo-tagged memories in $year'),
      builder: (context, data) => _MapView(data: data, baseUrl: baseUrl),
    );
  }
}

class _MapView extends StatelessWidget {
  const _MapView({required this.data, required this.baseUrl});

  final MapData data;
  final String baseUrl;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Stack(
      children: [
        FlutterMap(
          options: MapOptions(
            initialCenter: _center(data.features),
            initialZoom: _zoom(data.features),
            minZoom: 1,
            maxZoom: 19,
          ),
          children: [
            TileLayer(
              urlTemplate: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
              userAgentPackageName: 'xyz.vandijke.traces',
              maxZoom: 19,
            ),
            MarkerLayer(
              markers: [
                for (final feature in data.features)
                  Marker(
                    point: LatLng(feature.latitude, feature.longitude),
                    width: 44,
                    height: 44,
                    child: GestureDetector(
                      onTap: () => _showFeature(context, feature),
                      child: _Pin(feature: feature, baseUrl: baseUrl),
                    ),
                  ),
              ],
            ),
            Align(
              alignment: Alignment.topLeft,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Chip(
                  avatar: const Icon(Icons.place, size: 16),
                  label: Text('${data.count} memories'),
                  backgroundColor: scheme.surface.withValues(alpha: 0.9),
                ),
              ),
            ),
            Align(
              alignment: Alignment.bottomRight,
              child: Padding(
                padding: const EdgeInsets.all(6),
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: scheme.surface.withValues(alpha: 0.75),
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: const Padding(
                    padding: EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                    child: Text(
                      '© OpenStreetMap contributors',
                      style: TextStyle(fontSize: 10),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ],
    );
  }

  void _showFeature(BuildContext context, MapFeature feature) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (context) => Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (feature.previewUrl.isNotEmpty)
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: Image.network(
                  absoluteUrl(baseUrl, feature.previewUrl),
                  height: 180,
                  width: double.infinity,
                  fit: BoxFit.cover,
                  errorBuilder: (context, error, stack) => const SizedBox(
                    height: 0,
                  ),
                ),
              ),
            const SizedBox(height: 12),
            Text(
              feature.title,
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 4),
            if (feature.date.isNotEmpty)
              Text(
                feature.date,
                style: Theme.of(context).textTheme.labelSmall,
              ),
            if (feature.location.isNotEmpty)
              Row(
                children: [
                  const Icon(Icons.place_outlined, size: 14),
                  const SizedBox(width: 4),
                  Expanded(
                    child: Text(
                      feature.location,
                      style: Theme.of(context).textTheme.labelSmall,
                    ),
                  ),
                ],
              ),
            if (feature.description.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(feature.description),
            ],
            const SizedBox(height: 8),
            Text(
              '${feature.latitude.toStringAsFixed(5)}, '
              '${feature.longitude.toStringAsFixed(5)}',
              style: Theme.of(context).textTheme.labelSmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _Pin extends StatelessWidget {
  const _Pin({required this.feature, required this.baseUrl});

  final MapFeature feature;
  final String baseUrl;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final thumbnail =
        feature.previewUrl.isEmpty ? '' : absoluteUrl(baseUrl, feature.previewUrl);
    return Container(
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: scheme.primary,
        border: Border.all(color: Colors.white, width: 2),
        boxShadow: const [
          BoxShadow(color: Colors.black26, blurRadius: 4, offset: Offset(0, 2)),
        ],
        image: thumbnail.isEmpty
            ? null
            : DecorationImage(
                image: NetworkImage(thumbnail),
                fit: BoxFit.cover,
                onError: (exception, stack) {},
              ),
      ),
      child: thumbnail.isEmpty
          ? const Icon(Icons.place, size: 20, color: Colors.white)
          : null,
    );
  }
}

LatLng _center(List<MapFeature> features) {
  if (features.isEmpty) return const LatLng(20, 0);
  var lat = 0.0;
  var lng = 0.0;
  for (final feature in features) {
    lat += feature.latitude;
    lng += feature.longitude;
  }
  return LatLng(lat / features.length, lng / features.length);
}

double _zoom(List<MapFeature> features) {
  if (features.isEmpty) return 2;
  if (features.length == 1) return 13;

  var minLat = features.first.latitude;
  var maxLat = minLat;
  var minLng = features.first.longitude;
  var maxLng = minLng;
  for (final feature in features) {
    minLat = math.min(minLat, feature.latitude);
    maxLat = math.max(maxLat, feature.latitude);
    minLng = math.min(minLng, feature.longitude);
    maxLng = math.max(maxLng, feature.longitude);
  }
  final span = math.max(maxLat - minLat, maxLng - minLng);
  if (span > 60) return 1.5;
  if (span > 25) return 3;
  if (span > 10) return 4;
  if (span > 4) return 5.5;
  if (span > 1.5) return 7;
  if (span > 0.5) return 8.5;
  if (span > 0.15) return 10;
  return 11.5;
}
