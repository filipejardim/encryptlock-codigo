import 'dart:convert';

import 'package:flutter/services.dart';

/// One explained point: an emoji, a title and a paragraph.
class GuideItem {
  const GuideItem(this.icon, this.title, this.body);

  final String icon;
  final String title;
  final String body;

  static List<GuideItem> listFrom(Object? json) => [
    for (final e in json as List) GuideItem((e as Map)['icon'] as String, e['title'] as String, e['body'] as String),
  ];
}

/// The long texts of the app (why EncryptLock, how to use it, who we are,
/// what LockKeepers are, how the blockchain works), one file per language in assets/guide/.
class Guide {
  Guide.fromJson(Map<String, dynamic> j)
    : whyTitle = j['why_title'] as String,
      whyIntro = j['why_intro'] as String,
      why = GuideItem.listFrom(j['why']),
      lkTitle = j['lk_title'] as String,
      lkBody = j['lk_body'] as String,
      earnTitle = j['earn_title'] as String,
      earnBody = j['earn_body'] as String,
      chainTitle = j['chain_title'] as String,
      chainIntro = j['chain_intro'] as String,
      chain = GuideItem.listFrom(j['chain']),
      howtoTitle = j['howto_title'] as String,
      howto = GuideItem.listFrom(j['howto']),
      aboutTitle = j['about_title'] as String,
      aboutBody = j['about_body'] as String,
      contactsTitle = j['contacts_title'] as String,
      contactsBody = j['contacts_body'] as String;

  final String whyTitle;
  final String whyIntro;
  final List<GuideItem> why;
  final String lkTitle;
  final String lkBody;
  final String earnTitle;
  final String earnBody;
  final String chainTitle;
  final String chainIntro;
  final List<GuideItem> chain;
  final String howtoTitle;
  final List<GuideItem> howto;
  final String aboutTitle;
  final String aboutBody;
  final String contactsTitle;
  final String contactsBody;

  static final _cache = <String, Future<Guide>>{};

  /// The texts for a language tag (as in AppLanguage), English if missing.
  static Future<Guide> load(String tag) => _cache[tag] ??= _read(tag);

  static Future<Guide> _read(String tag) async {
    String raw;
    try {
      raw = await rootBundle.loadString('assets/guide/$tag.json');
    } catch (_) {
      raw = await rootBundle.loadString('assets/guide/en.json');
    }
    return Guide.fromJson(jsonDecode(raw) as Map<String, dynamic>);
  }
}
