import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/storage/app_storage.dart';
import '../data/storage/key_value_store.dart';

/// Overridden in tests with [InMemoryKeyValueStore].
final keyValueStoreProvider = Provider<KeyValueStore>(
  (ref) => SecureKeyValueStore(),
);

final appStorageProvider = Provider<AppStorage>(
  (ref) => AppStorage(ref.watch(keyValueStoreProvider)),
);
