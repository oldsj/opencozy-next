# OpenCozy Display for Android

A thin full-screen WebView for a wall display (an Android tablet or frame). It opens
`/display` on your OpenCozy server; that page pairs itself with a code the first time and keeps its
session in the WebView's cookies. The app holds nothing else: no household data, no credentials,
no JavaScript bridge.

## What it does

- **First start:** asks for the server's address (`family.example.test`, HTTPS only, no path) and
  saves it. Then the display shows a pairing code; enter it under Manage → Displays.
- **Kiosk:** full screen, screen kept on, Back moves within the display's pages but never closes it.
- **Only your server:** pages on the saved origin load; any other host, `http:`, `file:`, `intent:`
  or other scheme is refused without opening another app. TLS errors are never bypassed; cleartext
  and mixed content are off. Backups are off, so the session can't be restored onto another device.
- **Unattended:** when the server can't be reached it says so and tries again every 30 seconds
  (a fresh load of `/display`, never a replay). If the page's renderer dies, the app restarts it.
- **Change server:** from the "Can't reach OpenCozy" screen; it also clears the session.

## Build

Needs JDK 17 and the Android SDK (platform 35). `./gradlew` downloads the pinned Gradle (checksum
verified).

```sh
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug   # app/build/outputs/apk/debug/
```

A release needs your own signing key, from the environment only (never commit it):

```sh
export OPENCOZY_RELEASE_KEYSTORE=/private/path/display.jks OPENCOZY_RELEASE_KEY_ALIAS=display
export OPENCOZY_RELEASE_STORE_PASSWORD=... OPENCOZY_RELEASE_KEY_PASSWORD=...
./gradlew :app:assembleRelease -PversionCode=2 -PversionName=0.1.1
```

`OPENCOZY_ORIGIN` (optional) only fills in the address field for a build made for one household.

Install with `adb install -r app/build/outputs/apk/release/app-release.apk`. Upgrades keep the
application ID `org.opencozy.display` and the same signing key, with a higher `versionCode`.

## Not covered by CI

CI builds the app, runs lint and the unit tests (`ServerAddress`). Pairing, cookies surviving a
restart, the retry screen and kiosk behaviour need a real device.
