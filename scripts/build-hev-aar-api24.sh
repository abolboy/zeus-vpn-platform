#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HEV_SOURCE_DIR="${HEV_SOURCE_DIR:?Set HEV_SOURCE_DIR to the pinned Hev source checkout}"
ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-${NDK_HOME:-}}"
ANDROID_NDK_HOME="${ANDROID_NDK_HOME:?Set ANDROID_NDK_HOME to Android NDK r27d}"
EXPECTED_HEV_COMMIT="75dbc1e9ace6c70d3d1b94df7c283efe1de7effc"
OUTPUT_AAR="$ROOT_DIR/apps/android/libs/hev-socks5-tunnel-2.20.0-api24.aar"
ABIS=(armeabi-v7a arm64-v8a x86 x86_64)

[[ -x "$ANDROID_NDK_HOME/ndk-build" ]] || { echo "ndk-build not found in $ANDROID_NDK_HOME" >&2; exit 1; }
[[ -f "$HEV_SOURCE_DIR/Android.mk" && -f "$HEV_SOURCE_DIR/Application.mk" ]] || { echo "Hev Android build files are missing" >&2; exit 1; }

actual_commit="$(git -C "$HEV_SOURCE_DIR" rev-parse HEAD)"
[[ "$actual_commit" == "$EXPECTED_HEV_COMMIT" ]] || {
  echo "Unexpected Hev source revision: expected $EXPECTED_HEV_COMMIT, got $actual_commit" >&2
  exit 1
}

submodule_status="$(git -C "$HEV_SOURCE_DIR" submodule status --recursive)"
[[ -n "$submodule_status" ]] || { echo "Expected Hev source submodules were not found" >&2; exit 1; }
if grep -Eq '^[-+U]' <<< "$submodule_status"; then
  echo "Hev source submodules are missing or do not match the pinned gitlinks:" >&2
  printf '%s\n' "$submodule_status" >&2
  exit 1
fi

if [[ -e "$HEV_SOURCE_DIR/jni" || -L "$HEV_SOURCE_DIR/jni" ]]; then
  echo "Refusing to replace existing Hev jni path" >&2
  exit 1
fi
ln -s . "$HEV_SOURCE_DIR/jni"
tmp_dir="$(mktemp -d)"
cleanup() {
  rm -f "$HEV_SOURCE_DIR/jni"
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

ndk_build="$ANDROID_NDK_HOME/ndk-build"
printf 'Building Hev 2.20.0 for Android API 24 with NDK %s\n' "$ANDROID_NDK_HOME"
(
  cd "$HEV_SOURCE_DIR"
  "$ndk_build" clean APP_PLATFORM=android-24 APP_ABI="${ABIS[*]}"
  "$ndk_build" APP_MODULES=hev-socks5-tunnel APP_PLATFORM=android-24 APP_ABI="${ABIS[*]}" APP_LDFLAGS=-Wl,--no-undefined -j"$(getconf _NPROCESSORS_ONLN)"
)

llvm_readelf="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/llvm-readelf"
[[ -x "$llvm_readelf" ]] || { echo "llvm-readelf not found in NDK" >&2; exit 1; }
declare -A triples=(
  [armeabi-v7a]=arm-linux-androideabi
  [arm64-v8a]=aarch64-linux-android
  [x86]=i686-linux-android
  [x86_64]=x86_64-linux-android
)

mkdir -p "$tmp_dir/aar/jni"
for abi in "${ABIS[@]}"; do
  library="$HEV_SOURCE_DIR/libs/$abi/libhev-socks5-tunnel.so"
  [[ -s "$library" ]] || { echo "Missing native library for $abi: $library" >&2; exit 1; }

  needed_libs="$("$llvm_readelf" -d "$library" | awk -F'[][]' '/\(NEEDED\)/ { print $2 }')"
  [[ -n "$needed_libs" ]] || { echo "No DT_NEEDED entries found for $abi; refusing incomplete audit" >&2; exit 1; }
  while IFS= read -r needed; do
    [[ -n "$needed" ]] || continue
    stub="$ANDROID_NDK_HOME/sysroot/usr/lib/${triples[$abi]}/24/$needed"
    [[ -f "$stub" ]] || {
      echo "Dependency $needed for $abi has no Android API 24 NDK stub at $stub" >&2
      exit 1
    }
  done <<< "$needed_libs"

  mkdir -p "$tmp_dir/aar/jni/$abi"
  install -m 0644 "$library" "$tmp_dir/aar/jni/$abi/libhev-socks5-tunnel.so"
done

mkdir -p "$tmp_dir/classes"
javac --release 8 -Xlint:-options -d "$tmp_dir/classes" "$HEV_SOURCE_DIR/android/hev/htproxy/TProxyService.java"
(cd "$tmp_dir/classes" && jar cf "$tmp_dir/aar/classes.jar" .)
cat > "$tmp_dir/aar/proguard.txt" <<'PROGUARD'
-keep class hev.htproxy.** { *; }
PROGUARD
cat > "$tmp_dir/aar/AndroidManifest.xml" <<'MANIFEST'
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="hev.socks5.tunnel">
  <uses-sdk android:minSdkVersion="24" />
</manifest>
MANIFEST

mkdir -p "$(dirname "$OUTPUT_AAR")"
rm -f "$OUTPUT_AAR"
(cd "$tmp_dir/aar" && zip -q -r "$OUTPUT_AAR" .)
unzip -t "$OUTPUT_AAR" >/dev/null
unzip -p "$OUTPUT_AAR" AndroidManifest.xml | grep -Fq 'android:minSdkVersion="24"'
for abi in "${ABIS[@]}"; do
  unzip -l "$OUTPUT_AAR" "jni/$abi/libhev-socks5-tunnel.so" | grep -Fq "jni/$abi/libhev-socks5-tunnel.so"
done
sha256sum "$OUTPUT_AAR"
echo "Built and audited Hev 2.20.0 AAR for Android API 24: $OUTPUT_AAR"
