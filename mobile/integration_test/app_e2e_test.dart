import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:traces/main.dart' as app;

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('app boots into the TRACES onboarding flow', (tester) async {
    await Future<void>(() => app.main());

    // The real app always renders its brand; the Flutter template counter demo
    // does not. Waiting for this also fails if the entry point regresses to the
    // template, which a plain WidgetsApp check would happily accept.
    final branded = find.text('TRACES');
    await _pumpUntil(tester, branded, const Duration(seconds: 30));

    expect(branded, findsWidgets,
        reason: 'The app must render the TRACES UI after launch');
    expect(find.text('Flutter Demo Home Page'), findsNothing);
    expect(tester.takeException(), isNull);
  });
}

/// Pumps frames until [finder] matches, unlike pumpAndSettle this also works on
/// screens with infinite animations (splash spinners, pulsing headers).
Future<void> _pumpUntil(
  WidgetTester tester,
  Finder finder,
  Duration timeout,
) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (tester.any(finder)) return;
    await tester.pump(const Duration(milliseconds: 100));
  }
}
