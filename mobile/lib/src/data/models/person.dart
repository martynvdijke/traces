import 'json_utils.dart';

/// A tagged person, mirroring `models.Person` on the server.
class Person {
  const Person({
    required this.id,
    required this.name,
    this.avatarUrl = '',
    this.bio = '',
    this.birthDate = '',
    this.color = '',
    this.eventCount,
    this.createdAt = '',
  });

  final int id;
  final String name;
  final String avatarUrl;
  final String bio;
  final String birthDate;
  final String color;
  final int? eventCount;
  final String createdAt;

  factory Person.fromJson(Map<String, dynamic> json) => Person(
        id: asInt(json['id']),
        name: asString(json['name']),
        avatarUrl: asString(json['avatar_url']),
        bio: asString(json['bio']),
        birthDate: asString(json['birth_date']),
        color: asString(json['color']),
        eventCount: asIntOrNull(json['event_count']),
        createdAt: asString(json['created_at']),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'avatar_url': avatarUrl,
        'bio': bio,
        'birth_date': birthDate,
        'color': color,
      };

  /// First letter of the name, for avatar placeholders.
  String get initial {
    final trimmed = name.trim();
    return trimmed.isEmpty ? '?' : trimmed[0].toUpperCase();
  }

  @override
  bool operator ==(Object other) => other is Person && other.id == id;

  @override
  int get hashCode => id.hashCode;
}
