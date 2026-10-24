import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../app_state.dart';
import '../l10n/gen/app_localizations.dart';
import '../theme.dart';
import '../widgets/brand.dart';

/// Receive: the address as a QR code (to point a camera at) and as text.
class ReceiveTab extends StatelessWidget {
  const ReceiveTab({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final state = AppScope.of(context);
    final address = state.address ?? '';
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Text(
          t.receiveTitle,
          textAlign: TextAlign.center,
          style: TextStyle(
            color: Brand.neonGreen,
            fontSize: 20,
            fontWeight: FontWeight.w700,
            shadows: [Shadow(color: Brand.neonGreen.withValues(alpha: 0.6), blurRadius: 12)],
          ),
        ),
        const SizedBox(height: 18),
        Center(
          child: Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(24),
              boxShadow: Brand.glow(Brand.neonGreen, 0.8),
            ),
            child: QrImageView(
              // A link: the phone's own camera offers "open in EncryptLock",
              // which opens Send with this address filled in.
              data: 'encryptlock:$address',
              size: 230,
              backgroundColor: Colors.white,
              eyeStyle: const QrEyeStyle(eyeShape: QrEyeShape.square, color: Color(0xFF0B3D1E)),
              dataModuleStyle: const QrDataModuleStyle(dataModuleShape: QrDataModuleShape.square, color: Colors.black),
            ),
          ),
        ),
        const SizedBox(height: 20),
        NeonCard(
          child: Column(
            children: [
              SelectableText(
                address,
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600, letterSpacing: 0.3),
              ),
              const SizedBox(height: 14),
              NeonButton(
                label: t.copy,
                icon: Icons.copy,
                secondary: true,
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: address));
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.copied)));
                },
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        Text(
          state.registering ? t.registering : t.receiveExplain,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Brand.pearl, height: 1.4),
        ),
      ],
    );
  }
}
