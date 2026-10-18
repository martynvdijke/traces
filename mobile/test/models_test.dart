import 'package:flutter_test/flutter_test.dart';
import 'package:traces/src/data/models/event_stats.dart';
import 'package:traces/src/data/models/map_data.dart';
import 'package:traces/src/data/models/person.dart';
import 'package:traces/src/data/models/person_events.dart';
import 'package:traces/src/data/models/tag_count.dart';
import 'package:traces/src/data/models/timeline_event.dart';
import 'package:traces/src/data/models/upload_result.dart';
import 'package:traces/src/data/models/wrapped_summary.dart';

void main() {
  group('TimelineEvent.fromJson', () {
    test('decodes a fully-populated server payload', () {
      final event = TimelineEvent.fromJson({
        'id': 42,
        'title': 'Beach day',
        'description': 'Sunny afternoon',
        'date': '2026-07-04',
        'location': 'Scheveningen',
        'media_type': 'image',
        'media_url': '/media/ab/hash.jpg',
        'thumbnail': '/media/ab/hash.thumb.jpg',
        'tags': 'sun, sea, beach',
        'is_favorite': true,
        'person_id': 3,
        'latitude': 52.1082,
        'longitude': 4.2734,
        'person': {'id': 3, 'name': 'Ada', 'color': '#fff'},
      });

      expect(event.id, 42);
      expect(event.title, 'Beach day');
      expect(event.date, '2026-07-04');
      expect(event.mediaType, 'image');
      expect(event.isFavorite, isTrue);
      expect(event.personId, 3);
      expect(event.latitude, closeTo(52.1082, 1e-9));
      expect(event.person?.name, 'Ada');
      expect(event.hasGeo, isTrue);
      expect(event.hasMedia, isTrue);
      expect(event.previewUrl, '/media/ab/hash.thumb.jpg');
      expect(event.tagList, ['sun', 'sea', 'beach']);
    });

    test('tolerates the nullable pointer fields the Go API emits', () {
      final event = TimelineEvent.fromJson({
        'id': 1,
        'title': 'Undated',
        'tags': '',
        'person_id': null,
        'latitude': null,
        'longitude': null,
        'person': null,
      });

      expect(event.personId, isNull);
      expect(event.latitude, isNull);
      expect(event.longitude, isNull);
      expect(event.person, isNull);
      expect(event.hasGeo, isFalse);
      expect(event.hasMedia, isFalse);
      expect(event.tagList, isEmpty);
    });

    test('accepts numbers encoded as strings', () {
      final event = TimelineEvent.fromJson({
        'id': '7',
        'latitude': '1.5',
        'longitude': '2.5',
      });
      expect(event.id, 7);
      expect(event.latitude, closeTo(1.5, 1e-9));
    });

    test('prefers the media URL when there is no thumbnail', () {
      final event = TimelineEvent.fromJson({'media_url': '/media/x.mp4'});
      expect(event.previewUrl, '/media/x.mp4');
    });
  });

  test('TimelineEvent.toJson sends the create payload', () {
    const event = TimelineEvent(
      title: 'Trip',
      date: '2026-01-02',
      tags: 'a, b',
      latitude: 1,
      longitude: 2,
      personId: 5,
    );
    final json = event.toJson();
    expect(json['id'], 0);
    expect(json['title'], 'Trip');
    expect(json['person_id'], 5);
    expect(json['latitude'], 1);
    expect(json['tags'], 'a, b');
  });

  test('Person.fromJson exposes an avatar initial', () {
    final person = Person.fromJson({'id': 9, 'name': 'ada lovelace'});
    expect(person.initial, 'A');

    final unnamed = Person.fromJson({'id': 10, 'name': ''});
    expect(unnamed.initial, '?');
  });

  test('PersonEvents.fromJson hydrates the person and their events', () {
    final personEvents = PersonEvents.fromJson({
      'person': {'id': 3, 'name': 'Ada'},
      'events': [
        {'id': 1, 'title': 'First steps', 'date': '2026-02-01'},
      ],
    });
    expect(personEvents.person.name, 'Ada');
    expect(personEvents.events.single.title, 'First steps');
  });

  test('MapData.fromJson reads the flat FeatureCollection shape', () {
    final data = MapData.fromJson({
      'type': 'FeatureCollection',
      'features': [
        {
          'id': 1,
          'title': 'Home',
          'latitude': 52.0,
          'longitude': 4.0,
          'thumbnail': '/media/t.jpg',
        },
        {'id': 2, 'title': 'No geo', 'latitude': null, 'longitude': null},
      ],
    });
    expect(data.count, 2);
    expect(data.features.first.latitude, closeTo(52.0, 1e-9));
    expect(data.features.first.previewUrl, '/media/t.jpg');
    expect(data.features[1].latitude, 0);
  });

  test('UploadResult.fromJson exposes EXIF geo when present', () {
    final withGeo = UploadResult.fromJson({
      'url': '/media/ab/x.jpg',
      'media_type': 'image',
      'latitude': 52.1,
      'longitude': 4.2,
      'location_suggestion': 'Scheveningen',
    });
    expect(withGeo.hasExifGeo, isTrue);
    expect(withGeo.locationSuggestion, 'Scheveningen');

    final withoutGeo = UploadResult.fromJson({'url': '/media/ab/y.jpg'});
    expect(withoutGeo.hasExifGeo, isFalse);
  });

  test('TagCount.fromJson and WrappedSummary.fromJson decode stats', () {
    expect(TagCount.fromJson({'name': 'beach', 'count': 3}).count, 3);

    final wrapped = WrappedSummary.fromJson({
      'year': 2026,
      'total_events': 12,
      'total_media': 5,
      'longest_streak': 4,
      'favorite_count': 2,
      'busiest_month': 'July',
      'busiest_month_count': 6,
      'by_month': {'7': 6},
    });
    expect(wrapped.totalEvents, 12);
    expect(wrapped.busiestMonth, 'July');
    expect(wrapped.byMonth['7'], 6);
  });

  test('EventStats.fromJson decodes the aggregates', () {
    final stats = EventStats.fromJson({
      'total': 20,
      'with_geo': 8,
      'with_media': 11,
      'person_count': 4,
      'total_years': 3,
      'by_month': {'1': 2},
      'by_tag': {'beach': 5},
    });
    expect(stats.total, 20);
    expect(stats.withGeo, 8);
    expect(stats.personCount, 4);
    expect(stats.totalYears, 3);
    expect(stats.byTag['beach'], 5);
  });
}
