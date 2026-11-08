import 'json_utils.dart';
import 'person.dart';

/// A single timeline entry, mirroring `models.TimelineEvent`.
///
/// `tags` is a single comma-separated string on the wire (never a JSON array),
/// and `latitude`/`longitude`/`person_id` are nullable. Events can be tagged
/// with several people: [personIds]/[people] hold the full set, while the
/// legacy [personId]/[person] mirror the first entry for compatibility.
class TimelineEvent {
  const TimelineEvent({
    this.id = 0,
    this.title = '',
    this.description = '',
    this.date = '',
    this.startTime = '',
    this.endTime = '',
    this.location = '',
    this.mediaType = '',
    this.mediaUrl = '',
    this.thumbnail = '',
    this.mediaCaption = '',
    this.tags = '',
    this.isPublic = false,
    this.isFavorite = false,
    this.createdAt = '',
    this.personId,
    this.latitude,
    this.longitude,
    this.person,
    this.personIds = const [],
    this.people = const [],
    this.recurring = '',
    this.weatherData = '',
    this.userId = 0,
  });

  final int id;
  final String title;
  final String description;
  final String date;
  final String startTime;
  final String endTime;
  final String location;
  final String mediaType;
  final String mediaUrl;
  final String thumbnail;
  final String mediaCaption;
  final String tags;
  final bool isPublic;
  final bool isFavorite;
  final String createdAt;
  final int? personId;
  final double? latitude;
  final double? longitude;
  final Person? person;
  final List<int> personIds;
  final List<Person> people;
  final String recurring;
  final String weatherData;
  final int userId;

  factory TimelineEvent.fromJson(Map<String, dynamic> json) {
    final personJson = json['person'];
    final legacyPersonId = asIntOrNull(json['person_id']);
    final legacyPerson = personJson is Map
        ? Person.fromJson(asMap(personJson))
        : null;

    final people = <Person>[
      for (final entry in asList(json['people']))
        if (entry is Map && asInt(asMap(entry)['id']) > 0)
          Person.fromJson(asMap(entry)),
    ];

    var personIds = <int>[
      for (final id in asList(json['person_ids']))
        if (asInt(id) > 0) asInt(id),
    ];

    // Older servers only sent a single person; mirror it into the new lists.
    if (personIds.isEmpty && legacyPersonId != null) {
      personIds = [legacyPersonId];
    }
    if (personIds.isEmpty && people.isNotEmpty) {
      personIds = people.map((person) => person.id).toList(growable: false);
    }

    final resolvedPeople = people.isNotEmpty || legacyPerson == null
        ? people
        : [legacyPerson];

    return TimelineEvent(
      id: asInt(json['id']),
      title: asString(json['title']),
      description: asString(json['description']),
      date: asString(json['date']),
      startTime: asString(json['start_time']),
      endTime: asString(json['end_time']),
      location: asString(json['location']),
      mediaType: asString(json['media_type']),
      mediaUrl: asString(json['media_url']),
      thumbnail: asString(json['thumbnail']),
      mediaCaption: asString(json['media_caption']),
      tags: asString(json['tags']),
      isPublic: asBool(json['is_public']),
      isFavorite: asBool(json['is_favorite']),
      createdAt: asString(json['created_at']),
      personId:
          legacyPersonId ?? (personIds.isNotEmpty ? personIds.first : null),
      latitude: asDoubleOrNull(json['latitude']),
      longitude: asDoubleOrNull(json['longitude']),
      person:
          legacyPerson ??
          (resolvedPeople.isNotEmpty ? resolvedPeople.first : null),
      personIds: List<int>.unmodifiable(personIds),
      people: List<Person>.unmodifiable(resolvedPeople),
      recurring: asString(json['recurring']),
      weatherData: asString(json['weather_data']),
      userId: asInt(json['user_id']),
    );
  }

  /// Payload for `POST /api/events` (create when [id] is 0, update otherwise).
  ///
  /// `person_ids` is authoritative on the server; `person_id` is still sent so
  /// older backend versions keep working.
  Map<String, dynamic> toJson() {
    final ids = personIds.isNotEmpty ? personIds : <int>[?personId];
    return {
      'id': id,
      'title': title,
      'description': description,
      'date': date,
      'start_time': startTime,
      'end_time': endTime,
      'location': location,
      'person_id': personId ?? (ids.isNotEmpty ? ids.first : null),
      'person_ids': ids,
      'media_type': mediaType,
      'media_url': mediaUrl,
      'tags': tags,
      'latitude': latitude,
      'longitude': longitude,
      'recurring': recurring,
      'user_id': userId,
      'weather_data': weatherData,
    };
  }

  /// Split [tags] into a de-duplicated, trimmed list.
  List<String> get tagList => tags
      .split(',')
      .map((t) => t.trim())
      .where((t) => t.isNotEmpty)
      .toSet()
      .toList(growable: false);

  bool get hasGeo => latitude != null && longitude != null;

  bool get hasMedia => mediaUrl.isNotEmpty;

  /// Best URL to render for a media item, preferring the thumbnail.
  String get previewUrl => thumbnail.isNotEmpty ? thumbnail : mediaUrl;

  TimelineEvent copyWith({
    int? id,
    String? title,
    String? description,
    String? date,
    String? startTime,
    String? endTime,
    String? location,
    String? mediaType,
    String? mediaUrl,
    String? thumbnail,
    String? mediaCaption,
    String? tags,
    bool? isPublic,
    bool? isFavorite,
    String? createdAt,
    int? personId,
    double? latitude,
    double? longitude,
    Person? person,
    List<int>? personIds,
    List<Person>? people,
    String? recurring,
    String? weatherData,
    int? userId,
  }) => TimelineEvent(
    id: id ?? this.id,
    title: title ?? this.title,
    description: description ?? this.description,
    date: date ?? this.date,
    startTime: startTime ?? this.startTime,
    endTime: endTime ?? this.endTime,
    location: location ?? this.location,
    mediaType: mediaType ?? this.mediaType,
    mediaUrl: mediaUrl ?? this.mediaUrl,
    thumbnail: thumbnail ?? this.thumbnail,
    mediaCaption: mediaCaption ?? this.mediaCaption,
    tags: tags ?? this.tags,
    isPublic: isPublic ?? this.isPublic,
    isFavorite: isFavorite ?? this.isFavorite,
    createdAt: createdAt ?? this.createdAt,
    personId: personId ?? this.personId,
    latitude: latitude ?? this.latitude,
    longitude: longitude ?? this.longitude,
    person: person ?? this.person,
    personIds: personIds ?? this.personIds,
    people: people ?? this.people,
    recurring: recurring ?? this.recurring,
    weatherData: weatherData ?? this.weatherData,
    userId: userId ?? this.userId,
  );
}
