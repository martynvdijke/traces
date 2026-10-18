import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/models/event_filter.dart';

void main() {
  group('EventFilter.toQueryParameters', () {
    test('is empty for a default filter', () {
      const filter = EventFilter();
      expect(filter.toQueryParameters(), {'sort': 'desc'});
    });

    test('sends year, zero-padded month, tag, person and media type', () {
      const filter = EventFilter(
        year: 2026,
        month: 3,
        tag: 'beach',
        personId: 7,
        mediaType: 'image',
      );
      expect(filter.toQueryParameters(), {
        'year': '2026',
        'month': '03',
        'tag': 'beach',
        'person_id': '7',
        'media_type': 'image',
        'sort': 'desc',
      });
    });

    test('includes a trimmed search query and omits blank ones', () {
      expect(
        const EventFilter(query: '  sunset  ').toQueryParameters()['q'],
        'sunset',
      );
      expect(
        const EventFilter(query: '   ').toQueryParameters().containsKey('q'),
        isFalse,
      );
    });
  });

  group('EventFilter.requiresSearchEndpoint', () {
    test('is false for the filters /api/events supports', () {
      const filter = EventFilter(year: 2026, month: 3, tag: 'beach');
      expect(filter.requiresSearchEndpoint, isFalse);
    });

    test('is true for query, person and media filters', () {
      expect(const EventFilter(query: 'sunset').requiresSearchEndpoint, isTrue);
      expect(const EventFilter(personId: 1).requiresSearchEndpoint, isTrue);
      expect(
        const EventFilter(mediaType: 'video').requiresSearchEndpoint,
        isTrue,
      );
    });
  });

  group('copyWith', () {
    test('clears optional filters when asked', () {
      const filter = EventFilter(month: 5, tag: 'x', personId: 2, mediaType: 'audio');
      final cleared = filter.copyWith(
        clearMonth: true,
        clearTag: true,
        clearPerson: true,
        clearMediaType: true,
      );
      expect(cleared.month, isNull);
      expect(cleared.tag, isNull);
      expect(cleared.personId, isNull);
      expect(cleared.mediaType, isNull);
    });

    test('keeps untouched values', () {
      const filter = EventFilter(year: 2026, tag: 'beach');
      expect(filter.copyWith(year: 2025).tag, 'beach');
    });
  });

  group('MediaCategory', () {
    test('maps wire values', () {
      expect(MediaCategory.fromWire('image'), MediaCategory.image);
      expect(MediaCategory.fromWire('boardgame'), MediaCategory.boardgame);
      expect(MediaCategory.fromWire(''), isNull);
      expect(MediaCategory.fromWire(null), isNull);
    });

    test('exposes the "all" category with an empty wire value', () {
      expect(MediaCategory.other.wireValue, '');
      expect(MediaCategory.image.label, 'Photos');
    });
  });
}
