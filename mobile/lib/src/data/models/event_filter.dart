/// Filter state for the year timeline.
///
/// Mirrors the query parameters accepted by `GET /api/events`
/// (`year`, `month`, `tag`, `user_id`, `sort`, `limit`) and
/// `GET /api/events/search` (`q`, `year`, `month`, `tag`, `person_id`,
/// `media_type`, `location`, `user_id`).
class EventFilter {
  const EventFilter({
    this.year,
    this.month,
    this.tag,
    this.personId,
    this.mediaType,
    this.query = '',
    this.favoritesOnly = false,
    this.sort = 'desc',
  });

  final int? year;
  final int? month;
  final String? tag;
  final int? personId;
  final String? mediaType;
  final String query;
  final bool favoritesOnly;
  final String sort;

  static const List<MediaCategory> categories = MediaCategory.values;

  bool get hasQuery => query.trim().isNotEmpty;

  /// The richer endpoint is required when a filter isn't supported by
  /// `GET /api/events`.
  bool get requiresSearchEndpoint =>
      hasQuery || personId != null || (mediaType != null && mediaType!.isNotEmpty);

  EventFilter copyWith({
    int? year,
    int? month,
    String? tag,
    int? personId,
    String? mediaType,
    String? query,
    bool? favoritesOnly,
    String? sort,
    bool clearMonth = false,
    bool clearTag = false,
    bool clearPerson = false,
    bool clearMediaType = false,
  }) =>
      EventFilter(
        year: year ?? this.year,
        month: clearMonth ? null : (month ?? this.month),
        tag: clearTag ? null : (tag ?? this.tag),
        personId: clearPerson ? null : (personId ?? this.personId),
        mediaType: clearMediaType ? null : (mediaType ?? this.mediaType),
        query: query ?? this.query,
        favoritesOnly: favoritesOnly ?? this.favoritesOnly,
        sort: sort ?? this.sort,
      );

  Map<String, dynamic> toQueryParameters() {
    final params = <String, dynamic>{};
    if (year != null) params['year'] = '$year';
    if (month != null) params['month'] = month!.toString().padLeft(2, '0');
    if (tag != null && tag!.isNotEmpty) params['tag'] = tag;
    if (personId != null) params['person_id'] = '$personId';
    if (mediaType != null && mediaType!.isNotEmpty) params['media_type'] = mediaType;
    if (sort.isNotEmpty) params['sort'] = sort;
    if (hasQuery) params['q'] = query.trim();
    return params;
  }

  @override
  bool operator ==(Object other) =>
      other is EventFilter &&
      other.year == year &&
      other.month == month &&
      other.tag == tag &&
      other.personId == personId &&
      other.mediaType == mediaType &&
      other.query == query &&
      other.favoritesOnly == favoritesOnly &&
      other.sort == sort;

  @override
  int get hashCode =>
      Object.hash(year, month, tag, personId, mediaType, query, favoritesOnly, sort);
}

/// Media categories exposed by the filter bar.
enum MediaCategory {
  image('image', 'Photos'),
  video('video', 'Videos'),
  audio('audio', 'Audio'),
  boardgame('boardgame', 'Games'),
  other('', 'All');

  const MediaCategory(this.wireValue, this.label);

  /// Value sent to the server (`''` means "no filter").
  final String wireValue;
  final String label;

  static MediaCategory? fromWire(String? value) {
    if (value == null || value.isEmpty) return null;
    for (final category in MediaCategory.values) {
      if (category.wireValue == value) return category;
    }
    return null;
  }
}
