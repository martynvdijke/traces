import 'json_utils.dart';
import 'person.dart';
import 'timeline_event.dart';

/// Response of `GET /api/persons/:id/events` — a person plus their milestones.
class PersonEvents {
  const PersonEvents({required this.person, this.events = const []});

  final Person person;
  final List<TimelineEvent> events;

  factory PersonEvents.fromJson(Map<String, dynamic> json) => PersonEvents(
        person: Person.fromJson(asMap(json['person'])),
        events: asMapList(json['events'])
            .map(TimelineEvent.fromJson)
            .toList(growable: false),
      );
}
