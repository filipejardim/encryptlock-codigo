<h1 align="center">EncryptLock: source code</h1>

<p align="center"><b>Public to verify. Not to copy.</b></p>

This is the source code of EncryptLock (LOCK): the LockKeeper program (the
software on each PC that runs the blockchain, creates the blocks and earns
LOCK), the wallet apps for Windows and Android, the Polygon swap vault and the
explorer. It is published so anyone can check that the apps do what we say,
and nothing else.

> ⚠️ **View-only license.** You may read it, build it to check it and report
> bugs. You may **not** copy it, modify it, reuse it or launch another network
> or coin with it, now or ever. See [LICENSE](LICENSE).

## Download the apps

Only from [encryptlock.pages.dev](https://encryptlock.pages.dev) or
[encryptlock-downloads](https://github.com/filipejardim/encryptlock-downloads).
The white paper with every rule is there too.

## What is where

| Folder | What it does |
|---|---|
| `app`, `x`, `protocol` | The blockchain rules (Cosmos SDK + CometBFT): transfers that must be accepted within 1 hour, limits, "Wasn't me", inheritance, Sovereign Vault, emission, market, the signing group |
| `chip` | 1 computer = 1 LockKeeper: the TPM 2.0 security chip check (maker certificate + secret test inside the chip; virtual chips refused) |
| `wallet`, `hybrid` | Keys from the 24 words and the double signature (Ed25519 + ML-DSA-65, post-quantum) |
| `cmd/lockkeeper` | The LockKeeper program |
| `mobilecore`, `mobile` | The wallet app (Flutter) and its Go core |
| `polygon`, `polygonwatch` | LOCK ↔ USDC trades on Polygon (Boltz swap vault, MIT) and how the network sees the payments |
| `explorer` | The block explorer |
| `i18n` | The 21 languages |

## Build to check

Go 1.27, Flutter and Node.js. `go test ./...` runs the network's tests;
`scripts/app-windows.ps1` and `scripts/app-android.ps1` build the apps.

## Found a problem?

Write to **encryptlockoficial@proton.me**. Security problems: please write
first, before telling anyone else.

⚠️ EncryptLock will **never** send you a message first and will **never** ask
for your 24 words.

<sub>Copyright © 2026 Filipe Jardim. All rights reserved.</sub>
