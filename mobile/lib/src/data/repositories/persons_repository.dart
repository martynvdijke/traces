import '../api/api_client.dart';
import '../models/json_utils.dart';
import '../models/person.dart';
import '../models/person_events.dart';

/// Person listing, profiles, and autocomplete.
class PersonsRepository {
  PersonsRepository(this._client);

  final ApiClient _client;

  Future<List<Person>> fetchPersons({String query = ''}) async {
    final data = await _client.getJson(
      '/api/persons',
      query: query.trim().isEmpty ? null : {'q': query.trim()},
    );
    return asMapList(data).map(Person.fromJson).toList(growable: false);
  }

  Future<PersonEvents> fetchPersonEvents(int personId) async {
    final data = await _client.getJson('/api/persons/$personId/events');
    return PersonEvents.fromJson(asMap(data));
  }

  Future<List<String>> autocomplete(String field, String query) async {
    final data = await _client.getJson(
      '/api/autocomplete',
      query: {'field': field, 'q': query},
    );
    return asList(data).map((value) => value.toString()).toList(growable: false);
  }
}
