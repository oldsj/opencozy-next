#!/usr/bin/env bash
# Builds the app and its extensions for the simulator and runs the unit tests, unsigned for any
# team: the build skips signing, and the tests use ad-hoc signing because an unsigned test host
# can't use the Keychain. CI runs this; it works the same locally (run from anywhere).
#
#   ios/scripts/ci.sh            # build + test on the first available iPhone simulator
#   SIMULATOR="iPhone 17" ios/scripts/ci.sh
set -euo pipefail

cd "$(dirname "$0")/.."
derived=build/DerivedData

xcodebuild -version
xcodegen generate

xcodebuild -project OpenCozy.xcodeproj -scheme OpenCozy \
  -destination 'generic/platform=iOS Simulator' -derivedDataPath "$derived" \
  CODE_SIGNING_ALLOWED=NO build

if [[ -z "${SIMULATOR:-}" ]]; then
  # The newest runtime's first iPhone.
  SIMULATOR=$(xcrun simctl list devices available --json | jq -r '
    [.devices | to_entries[] | select(.key | test("SimRuntime\\.iOS-"))
      | {v: (.key | capture("iOS-(?<a>[0-9]+)-(?<b>[0-9]+)") | [(.a|tonumber), (.b|tonumber)]),
         d: .value[]}
      | select(.d.name | startswith("iPhone"))]
    | sort_by(.v) | last | .d.udid // empty')
  [[ -n "$SIMULATOR" ]] || { echo "No iPhone simulator is available." >&2; exit 1; }
  dest="platform=iOS Simulator,id=$SIMULATOR"
else
  dest="platform=iOS Simulator,name=$SIMULATOR"
fi

rm -rf build/Tests.xcresult
status=0
xcodebuild -project OpenCozy.xcodeproj -scheme OpenCozy -destination "$dest" \
  -derivedDataPath "$derived" -resultBundlePath build/Tests.xcresult \
  CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual DEVELOPMENT_TEAM= test || status=$?

# One simulator at a time: don't leave it running.
xcrun simctl shutdown all || true
exit "$status"
