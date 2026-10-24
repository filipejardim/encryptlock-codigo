#!/usr/bin/env bash
# Builds the Go wallet core (package mobilecore) as an Android library for the
# app: mobile/android/app/libs/mobilecore.aar.
# Needs: go install golang.org/x/mobile/cmd/gomobile@latest (and gobind),
#        the Android SDK with NDK 28.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PATH:/c/Program Files/Go/bin:$HOME/go/bin"
export ANDROID_HOME="${ANDROID_HOME:-$LOCALAPPDATA/Android/Sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/28.2.13676358}"
mkdir -p mobile/android/app/libs
# -checklinkname=0: a dependency used on Android (wlynxg/anet) needs it with
# recent Go versions.
gomobile bind -target=android/arm64,android/amd64 -androidapi 24 -javapkg com.encryptlock \
  -ldflags="-checklinkname=0 -s -w" \
  -o mobile/android/app/libs/mobilecore.aar ./mobilecore
