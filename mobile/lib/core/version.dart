/// This app's version (the same as pubspec.yaml; a test checks it).
const appVersion = '1.0.9';

/// Where the website says which version is the newest and the SHA-256 of
/// its files (core/updater.dart). Published with the site (site/versao.json).
final versionUrl = Uri.parse('https://encryptlock.pages.dev/versao.json');

/// [a] is a newer version than [b] ("1.0.10" is newer than "1.0.9").
bool isNewer(String a, String b) {
  List<int> parts(String v) => [for (final p in v.split('.')) int.tryParse(p) ?? 0];
  final x = parts(a), y = parts(b);
  for (var i = 0; i < 4; i++) {
    final p = i < x.length ? x[i] : 0, q = i < y.length ? y[i] : 0;
    if (p != q) return p > q;
  }
  return false;
}
