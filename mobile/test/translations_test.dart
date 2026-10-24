import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Every language must have every text of the English base, with the same
/// {placeholders}. Fails if a translation is missing.
void main() {
  final dir = Directory('lib/l10n');
  Map<String, dynamic> read(File f) => jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
  Iterable<String> texts(Map<String, dynamic> arb) => arb.keys.where((k) => !k.startsWith('@'));
  Set<String> placeholders(String s) => RegExp(r'\{(\w+)\}').allMatches(s).map((m) => m.group(1)!).toSet();

  final base = read(File('lib/l10n/app_en.arb'));
  final files = dir.listSync().whereType<File>().where((f) => f.path.endsWith('.arb')).toList();

  test('there are 21 languages', () {
    expect(files.length, 21);
  });

  for (final file in files) {
    test('${file.uri.pathSegments.last} has every text', () {
      final arb = read(file);
      for (final key in texts(base)) {
        expect(arb.containsKey(key), isTrue, reason: 'missing "$key"');
        final value = arb[key] as String;
        expect(value.trim(), isNotEmpty, reason: 'empty "$key"');
        expect(placeholders(value), placeholders(base[key] as String), reason: 'placeholders of "$key"');
      }
      for (final key in texts(arb)) {
        expect(base.containsKey(key), isTrue, reason: '"$key" does not exist in English');
      }
    });
  }
}
