import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/app.dart';
import 'package:traces/src/data/models/timeline_event.dart';
import 'package:traces/src/data/storage/key_value_store.dart';
import 'package:traces/src/features/timeline/event_card.dart';
import 'package:traces/src/features/timeline/media_lightbox.dart';
import 'package:traces/src/state/providers.dart';
import 'package:traces/src/widgets/async_value_view.dart';
import 'package:traces/src/widgets/year_selector.dart';

/// Pumps [child] inside a ProviderScope whose storage is a fresh in-memory
/// store (so no secure-storage platform channel is touched).
Future<void> pumpWithStore(WidgetTester tester, Widget child) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        keyValueStoreProvider.overrideWithValue(InMemoryKeyValueStore()),
      ],
      child: child,
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('onboarding asks for a server address', (tester) async {
    await pumpWithStore(tester, const TracesApp());

    expect(find.byKey(const Key('server-url-field')), findsOneWidget);
    expect(find.text('TRACES'), findsOneWidget);
  });

  testWidgets('onboarding rejects a malformed server address', (tester) async {
    await pumpWithStore(tester, const TracesApp());

    await tester.enterText(
      find.byKey(const Key('server-url-field')),
      'ftp://nope',
    );
    await tester.tap(find.byKey(const Key('connect-button')));
    await tester.pumpAndSettle();

    expect(find.textContaining('valid server address'), findsOneWidget);
  });

  testWidgets('AsyncValueView renders data, loading and error states',
      (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: AsyncValueView<String>(
          value: AsyncValue.data('hello'),
          builder: _textBuilder,
        ),
      ),
    );
    expect(find.text('hello'), findsOneWidget);

    await tester.pumpWidget(
      const MaterialApp(
        home: AsyncValueView<String>(
          value: AsyncValue.loading(),
          builder: _textBuilder,
        ),
      ),
    );
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(
      MaterialApp(
        home: AsyncValueView<String>(
          value: AsyncValue.error('boom', StackTrace.empty),
          builder: _textBuilder,
        ),
      ),
    );
    await tester.pump();
    expect(find.textContaining('boom'), findsOneWidget);
    expect(find.text('Retry'), findsNothing);
  });

  testWidgets('AsyncValueView shows the empty state for empty data',
      (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: AsyncValueView<List<String>>(
          value: AsyncValue.data(<String>[]),
          isEmpty: _isEmptyList,
          builder: _listBuilder,
        ),
      ),
    );
    expect(find.text('Nothing to show yet'), findsOneWidget);
  });

  testWidgets('YearSelector shows and updates the shared year', (tester) async {
    final currentYear = DateTime.now().year;
    await pumpWithStore(
      tester,
      MaterialApp(
        home: Scaffold(
          appBar: AppBar(actions: const [YearSelector()]),
        ),
      ),
    );

    expect(find.text('$currentYear'), findsOneWidget);

    await tester.tap(find.text('$currentYear'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('${currentYear - 1}').last);
    await tester.pumpAndSettle();

    expect(find.text('${currentYear - 1}'), findsOneWidget);
  });

  testWidgets('EventCard renders the memory details', (tester) async {
    const event = TimelineEvent(
      id: 1,
      title: 'Beach day',
      description: 'Sunny afternoon',
      date: '2026-07-04',
      location: 'Scheveningen',
      tags: 'sun, sea',
      isFavorite: true,
    );

    await pumpWithStore(
      tester,
      const MaterialApp(home: Scaffold(body: EventCard(event: event))),
    );

    expect(find.text('Beach day'), findsOneWidget);
    expect(find.text('Sunny afternoon'), findsOneWidget);
    expect(find.text('2026-07-04'), findsOneWidget);
    expect(find.text('Scheveningen'), findsOneWidget);
    expect(find.text('sun'), findsOneWidget);
    expect(find.text('sea'), findsOneWidget);
    expect(find.byIcon(Icons.more_vert), findsOneWidget);
  });

  testWidgets('EventCard can open the media lightbox', (tester) async {
    const event = TimelineEvent(
      id: 5,
      title: 'Holiday clip',
      date: '2026-08-01',
      mediaType: 'video',
      mediaUrl: '/media/ab/clip.mp4',
    );

    await pumpWithStore(
      tester,
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => EventCard(
              event: event,
              onOpenMedia: () => MediaLightbox.show(
                context,
                items: const [event],
                initialIndex: 0,
                baseUrl: 'http://localhost:6270',
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Holiday clip'));
    await tester.pumpAndSettle();

    expect(find.byType(MediaLightbox), findsOneWidget);
    expect(find.text('1 / 1'), findsOneWidget);
  });
}

Widget _textBuilder(BuildContext context, String data) => Text(data);

Widget _listBuilder(BuildContext context, List<String> data) =>
    Text('${data.length}');

bool _isEmptyList(List<String> data) => data.isEmpty;
