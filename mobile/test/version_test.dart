import 'dart:io';

import 'package:encryptlock_wallet/core/version.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('appVersion is the version in pubspec.yaml', () {
    final line = File('pubspec.yaml').readAsLinesSync().firstWhere((l) => l.startsWith('version:'));
    expect(line.split(':')[1].trim().split('+').first, appVersion);
  });

  test('newer versions', () {
    expect(isNewer('1.0.1', '1.0.0'), isTrue);
    expect(isNewer('1.0.10', '1.0.9'), isTrue);
    expect(isNewer('2', '1.9.9'), isTrue);
    expect(isNewer('1.0.0', '1.0.0'), isFalse);
    expect(isNewer('1.0', '1.0.0'), isFalse);
    expect(isNewer('0.9.9', '1.0.0'), isFalse);
  });
}
