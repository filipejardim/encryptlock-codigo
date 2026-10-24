import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Every language has the whole guide (why, how to use, about), with the same
/// number of points as English, and the texts never talk about other coins.
void main() {
  Map<String, dynamic> read(File f) => jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
  final base = read(File('assets/guide/en.json'));
  final files = Directory('assets/guide').listSync().whereType<File>().where((f) => f.path.endsWith('.json')).toList();
  final tags = Directory('lib/l10n')
      .listSync()
      .whereType<File>()
      .map((f) => f.uri.pathSegments.last)
      .where((n) => n.endsWith('.arb'))
      .map((n) => n.substring(4, n.length - 4))
      .toSet();

  test('one guide for each of the 21 languages', () {
    expect(files.map((f) => f.uri.pathSegments.last.replaceAll('.json', '')).toSet(), tags);
  });

  for (final file in files) {
    test('${file.uri.pathSegments.last} is complete', () {
      final g = read(file);
      expect(g.keys.toSet(), base.keys.toSet());
      for (final key in base.keys) {
        if (base[key] is String) {
          expect((g[key] as String).trim(), isNotEmpty, reason: key);
        } else {
          final items = g[key] as List;
          expect(items.length, (base[key] as List).length, reason: key);
          for (var i = 0; i < items.length; i++) {
            final item = items[i] as Map;
            expect(item['icon'], (base[key] as List)[i]['icon'], reason: '$key[$i] icon');
            expect((item['title'] as String).trim(), isNotEmpty, reason: '$key[$i] title');
            expect((item['body'] as String).trim(), isNotEmpty, reason: '$key[$i] body');
          }
        }
      }
      final all = file.readAsStringSync().toLowerCase();
      for (final word in ['bitcoin', 'btc', 'ethereum']) {
        expect(all.contains(word), isFalse, reason: 'mentions $word');
      }
    });
  }
}
