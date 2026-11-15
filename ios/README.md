# OpenCozy for iOS

A thin native shell around the OpenCozy web app, with two roles chosen on first run. Almost all UI ships from the server, so it changes without an app update. The app keeps native only what Apple requires to be native.

- **A child's device.** The web app's kid pages (today's chores and routines, points, rewards and screen-time rewards) in a web view, signed in as this device's child only. Natively, the app keeps chosen apps locked until the child spends points on screen time, and they lock again when the time runs out. A PIN-protected **Parent** button opens on-device setup: pairing, the app picker and unpairing.
- **A parent's device.** The whole web app (inbox, manage, everything) in a web view, signed in as the adult. Natively, push notifications with **Approve** and **Reject** (and the Ask-for-more-time answers) that work from the notification and from an Apple Watch, without unlocking the iPhone. Set Show Previews to When Unlocked to hide them on a locked iPhone. This role never touches Family Controls.

The OpenCozy server is the source of truth for the unlock window, the device's child and the inbox. Screen time uses Apple's **Family Controls** (authorization and the app picker), **ManagedSettings** (shields) and **DeviceActivity** (re-locking on schedule).

> **Status.** This is the app as it was in the previous OpenCozy server's repository, brought over
> unchanged apart from build fixes. It still calls that server's API (`/api/v1/...`, below), which
> this server does not serve. It moves onto this server's API (`api/`) as each slice lands: the
> child's device with slice 7 (devices), the parent's device with slices 10 and 11. Until then it
> builds and its unit tests pass, but it can't pair or sign in against this server.

## Layout

| Path | What it is |
| --- | --- |
| `project.yml` | XcodeGen spec, the source of truth. The generated `OpenCozy.xcodeproj` is gitignored. |
| `Config/OpenCozy.xcconfig` | Placeholder bundle ID, App Group and web host. Includes `Config/Local.xcconfig` if it exists. |
| `App/` | Role choice, kid home (web view plus the Parent button), parent PIN and setup, API client. |
| `App/Web/` | The web view shell: navigation policy, the `opencozy` bridge, the offline state, the kid web-session exchange. |
| `App/Pairing/` | Secure Enclave device key, request signing, QR scanner, pairing UI. |
| `App/Parent/` | Parent role: server address, the web view, the notification session (password or passkey), push payloads and notification actions. |
| `Shared/` | Unlock state in the App Group, shield logic, schedule planning. Compiled into the app and all three extensions. |
| `Monitor/` | DeviceActivityMonitor extension. Re-applies the shield when a window ends. |
| `ShieldConfiguration/` | Custom shield: "*App* is locked", the household's lock message (Manage → Devices), when screen time opens and the banked time, from the last sync (`Shared/ShieldText.swift`). |
| `ShieldAction/` | Shield buttons. **One more minute** (once per lock after a window) lifts the shield for 60 seconds offline and queues the event for the next sync; otherwise **OK** closes the app. **Ask for more time** queues a request in the App Group (this extension can't sign requests); on iOS 26.5 and later it also opens this app, which sends it at once, and earlier the next sync sends it. Shares the App Group and `Shared/`. |
| `Tests/` | Swift Testing unit tests and their fictional fixtures. |
| `Design/AppIcon.svg` | Source of the app icon (logo A). The command to re-render the PNG is in the file. |

## Build

Requires Xcode 26 or later (CI uses the `macos-26` runner's default Xcode; also checked with Xcode 27.2) and [XcodeGen](https://github.com/yonaskolb/XcodeGen).

```sh
ios/scripts/ci.sh   # generate, build for the simulator, run the unit tests, shut the simulator down
```

That is what CI runs (`.github/workflows/ios.yml`). By hand:

```sh
cd ios
xcodegen generate
xcodebuild -project OpenCozy.xcodeproj -scheme OpenCozy \
  -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
xcodebuild -project OpenCozy.xcodeproj -scheme OpenCozy \
  -destination 'platform=iOS Simulator,name=<an installed iPhone simulator>' \
  CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual DEVELOPMENT_TEAM= test
```

The tests use ad-hoc simulator signing rather than `CODE_SIGNING_ALLOWED=NO`, because an unsigned test host can't use the Keychain, which the PIN, pairing and parent-session tests need.

### Signing

Never commit a team ID or a real host. Copy `Config/Local.xcconfig.example` to `Config/Local.xcconfig` (gitignored) and set:

```
DEVELOPMENT_TEAM = YOURTEAMID
OPENCOZY_BUNDLE_ID = com.yourdomain.opencozy
OPENCOZY_APP_GROUP = group.com.yourdomain.opencozy
OPENCOZY_WEB_HOST = opencozy.yourdomain.example
```

Set `OPENCOZY_WEB_HOST` to the canonical host *after* any host rename. It's the passkey relying party, and passkeys enrolled under an old host stop working.

Then regenerate the project. The extensions use `$(OPENCOZY_BUNDLE_ID).monitor`, `.shield-configuration` and `.shield-action`.

Capabilities:
- **Family Controls** on all four targets.
- **App Groups** on the app and the monitor, with the same group.
- **Push Notifications** on the app. There's no setting for the push environment. At runtime the app reads `aps-environment` from its embedded provisioning profile and sends that with its push token: `development` for Xcode installs (the server uses APNs' sandbox host), `production` for ad-hoc. TestFlight and App Store installs have no embedded profile and always report `production`.
- **Associated Domains** on the app: `webcredentials:$(OPENCOZY_WEB_HOST)?mode=developer`, for parent passkeys. See [Passkeys on a LAN-only server](#passkeys-on-a-lan-only-server).

With automatic signing, Xcode registers the IDs.

- **Development** builds can use the Family Controls (Development) entitlement once the team is a paid Apple Developer Program member.
- **TestFlight and App Store** builds need Apple to approve the Family Controls *distribution* entitlement, [requested per bundle ID](https://developer.apple.com/contact/request/family-controls-distribution) including each extension. Ad-hoc or development installs are enough for one household.

### Passkeys on a LAN-only server

Normally Apple's CDN fetches `/.well-known/apple-app-site-association` from the internet and hands it to the device. OpenCozy only answers on the home network, so the CDN can never reach it. Instead, `?mode=developer` makes a **development-signed** install fetch the file straight from the server, over the home network or VPN. Nothing needs to be exposed publicly.

On each parent phone that will use a passkey:
1. Turn on **Settings → Privacy & Security → Developer Mode**, and restart when asked. The switch appears after Xcode has installed an app on the phone once.
2. Turn on **Settings → Developer → Associated Domains Development**.
3. Install the app from Xcode. The phone then fetches the association file from the server; its HTTPS certificate must be one the phone trusts.

The server must serve the file listing `<team ID>.<bundle ID>` under `webcredentials`. `mode=developer` is ignored in TestFlight and App Store builds, which still need a publicly reachable association file. Email and password sign-in works without any of this.

## Setting up a child's device

Prerequisites: the device is signed in with a **child Apple Account** that belongs to the parent's Family Sharing group. Screen Time on the device is managed by the family.

1. **Install** the app from Xcode and choose **A child's device**.
2. **Authorize Screen Time.** Tap **Ask a parent to approve**. The app calls `AuthorizationCenter.shared.requestAuthorization(for: .child)`. A parent or guardian in the family approves with their Apple Account on the device. Apple's docs say that `.individual` authorization lifts the restrictions that stop a user bypassing parental controls, such as deleting the app. That implies `.child` keeps them. Confirm on a device.
3. **Create a parent PIN.** Tap **Parent** (top right). The first time, you set a 4–8 digit PIN. It is stored as a salted hash in the Keychain and gates the setup screen. After five wrong attempts, entry locks for 30 seconds, doubling with each further miss up to an hour. The count lives in the Keychain, so closing the sheet or relaunching doesn't reset it.
4. **Pair.** In OpenCozy on another screen, open **Manage → Devices → Pair a device** and choose the child. On the device, in **Parent setup**, tap **Scan pairing code** and point the camera at the QR code. You can also tap **Enter the code instead** and type the address and the 8-character code. The code works once, for 5 minutes.
   - The app creates a P-256 key in the Secure Enclave and sends only its public key, with a signature over the pairing code and that key to prove it holds the private half. The server returns the device and its child.
   - Every API request is then signed with that key. No password, API key or bearer token is ever stored on the device.
   - The server address must be `https`. Plain `http` is refused, except for `localhost` when testing on the simulator.
   - A bearer API key left over from the Phase 1 app is deleted from the Keychain on first launch, and again on pairing. Revoke it on the server too, in OpenCozy under **Manage → Integrations**.
5. **Choose apps to lock.** Tap **Choose apps to lock…** and pick apps and categories in Apple's picker. **Don't include OpenCozy**, or a category that contains it, or the child can't open it to spend points. Add Safari and Chrome under **Always allowed**. The selection is shielded right away. An empty selection is refused. Websites are never shielded, so browsers stay open (school sites, Wikipedia): picking a category doesn't block its websites, and websites picked here are ignored. To stop locking altogether, remove OpenCozy's Screen Time access in Settings.
6. **Test the lock extension.** In **Status**, tap **Test the lock extension**. The app arms a throwaway schedule, and the monitor extension answers through the App Group. A pass proves the extension runs and that both share the same App Group. A failure usually means the App Group differs between the app and the monitor.
7. **Add screen-time rewards** in OpenCozy under **Manage → Rewards**, with *screen-time minutes* and target **personal** (the child's own devices). The kid pages offer them.

Adults can rename the device, reassign it to another child or revoke it in **Manage → Devices**. A reassignment takes effect at the next sync. The app drops the previous child's state, locks, and then applies the new child's screen time. A revoked or unassigned device fails closed.

## Setting up a parent's device

1. **Install** the app and choose **A parent's device**.
2. **Connect.** Enter the OpenCozy address (https). The web app opens; sign in there as usual, with a passkey or email and password. The web session is the normal cookie session and survives relaunches.
3. **Turn on notifications.** Sign in to OpenCozy in the app, then allow notifications when asked. Lock-screen actions can't use the web view's cookie, so the phone keeps its own parent session for push: a per-device token in the Keychain only, which expires after 90 days. The app gets it without a second sign-in: on the first page after launch, and each time the web view leaves `/login`, it sends `POST /api/parent-sessions` with body `{ deviceName }`. The app sends it natively, not from the page: it reads the web view's cookies from `WKHTTPCookieStore` (HttpOnly included), sends them with the canonical `Origin`, and the token never reaches page JavaScript. It refuses to send if a kid device web-session cookie is in the jar. A 401 or 403 (not signed in on the web), a 404 or 405 (an older server) or a network failure is silent. The manual sign-in under the bell (top right), with a passkey or email and password, remains the fallback. The request carries no `Sec-Fetch-*` headers (the server refuses any `Sec-Fetch-Site` other than `none`, so a page can't mint). **Stop notifications** in the same sheet removes the session and keeps web sign-ins from turning it back on until you sign in there again. The same applies when an adult revokes the phone: the next request with its token gets 401 (checked at launch and on every foreground), and the app clears the token and stops minting automatically, across relaunches, until someone signs in from the bell sheet. The session is named after the phone's own name when iOS provides it. Without Apple's user-assigned-device-name entitlement, iOS only says "iPhone", so the app suggests the model and the date instead, for example "iPhone (iPhone17,3), 7 Nov 2026". You can change it under the bell. You can also revoke it in OpenCozy under **Manage → Displays & devices**.
4. **Approve.**
   - In the app, the inbox is the web app's.
   - From a notification, **Approve** and **Reject** run in the background without unlocking, including from an Apple Watch. If one doesn't go through, a follow-up notification says why.
   - Both parents share one inbox, and the first decision wins. A stale action is answered with an **Already handled** notification.
   - When an item is settled elsewhere, the server sends a background push. The app removes that item's notification from the lock screen and updates the badge, so a stale Approve or Reject can't be tapped later. Apple throttles background pushes, so this is best effort.
   - The server is LAN-only. Away from home without the VPN, the app says **Can't reach OpenCozy. Are you home or on the VPN?** with **Try again**.

## How it works

### The web view

- **One origin.** The web view loads only the paired (kid) or connected (parent) origin: same scheme, host and port. A link elsewhere opens in Safari; `mailto:` and `tel:` go to the system; frames from other origins and anything else are dropped. `target=_blank` and `window.open` follow the same rules in the same web view. JavaScript `alert`, `confirm` and `prompt` show native alerts, so the web app's confirmations work.
- **Cookies persist** in the default website data store, shared by the app's web views.
- **Web data never crosses roles.** The server prefers an adult cookie over the kid device's, so a parent sign-in left in the store would make the kid web view act as the adult. The app wipes every cookie and all storage (`removeData(ofTypes: allWebsiteDataTypes())`):
  - at once, when the role changes, on unpair, when the device is locked by the server (401/403), and when a parent phone changes server;
  - before any web view loads for a different owner than last time (kid device ID and server, or parent and server). A stamp in `UserDefaults` makes this hold across crashes and relaunches. On a kid device this runs before the first exchange of each pairing.
- **Parent home:** a parent phone opens `/manage/inbox`; signed out, that page offers sign-in.
- **Web Inspector** is off in every build unless `SWIFT_ACTIVE_COMPILATION_CONDITIONS = $(inherited) OPENCOZY_WEB_INSPECTOR` is in `Config/Local.xcconfig`. Household installs are Xcode Debug builds, so `DEBUG` alone doesn't enable it.
- **Offline:** a page that can't load shows **Can't reach OpenCozy. Are you home or on the VPN?** with **Try again**. Pull to refresh reloads.
- **User agent:** the app appends `OpenCozy-iOS`, so the web app can tell it's in the app.

### Kid web session (child's device)

- On launch, the app makes a signed `POST /api/v1/device/web-session` (body `{}`) and loads the one-time exchange URL it returns. That sets a child-device session cookie scoped to this device's current child. The URL is accepted only if it's `/device/session` on the paired origin.
- The server answers `201 { url, expiresAt }`; the URL works once, within 60 seconds, then redirects to `/kid`. The session lasts 24 hours.
- **Session ended:** when the web view reaches `/device/expired` (the server's signal, whether by a full load or a client-side route change), lands on `/login`, or a page answers 401, the app gets a new exchange URL instead of showing that page. The child never signs in by hand. More than 3 exchanges in 2 minutes stop with an explanation and **Try again**, so a server that keeps refusing the cookie can't loop.
- A 401 or 403 from the exchange locks the device, as a sync does. A 404 means the server predates the web shell and says so.
- **Bridge:** the page may call `window.webkit.messageHandlers.opencozy.postMessage({type: "screenTimeChanged"})` after a screen-time redemption or after spending banked iPad time (feature-detect it). The app then re-syncs shields at once. Only that message is accepted, only from the main frame on the paired origin. The app also re-syncs whenever a page finishes loading, on pull to refresh, every 60 seconds and on returning to the foreground, so shields still follow a page that doesn't send the message.

### Signed requests (child's device)

- Every request carries `X-OpenCozy-Device`, `-Timestamp`, `-Nonce` and `-Signature` and no `Authorization` header. The signature is an ECDSA P-256 signature, made by the Secure Enclave key, over the contract's canonical `OPENCOZY-DEVICE-V1` string: method, path and query as sent, timestamp, nonce, the SHA-256 of the body, and the device ID.
- The Secure Enclave key works after the first unlock since boot. It never leaves the chip and is never backed up or synced. Pairing again replaces it only once the server accepts the new key.
- Before signing, any query-string character outside RFC 3986's unreserved set is percent-encoded, and that exact URL is sent. The server signs over WHATWG `url.search`, which would otherwise encode some characters (such as `'`) that Foundation leaves raw.
- The server accepts timestamps within 60 seconds of its own time. The app tracks the offset from `serverTime` on every sync. After a 401, it also checks the response's `Date` header; if that's more than 30 seconds off, it corrects the offset and retries once.
- Every sync starts with `GET /api/v1/device`, and the app renders for whichever child the server reports.

### Screen time (child's device)

- **Redeeming** happens in the web view (the web app's rewards page). The app only reads the result: see the bridge above.
- **Syncing:** the app syncs on launch, on returning to the foreground, every 60 seconds while it's on screen, when the page reports a redemption or finishes loading, and when a window ends. A sync requested while one is running is queued, not dropped. Responses are ordered, so an older in-flight response can't overwrite a newer window. A reward redeemed elsewhere, such as on the wall display, therefore reaches the device within a minute while OpenCozy is open, or when it's next opened.
- **Revoked or unassigned device:** a 401 or 403 from the API, or a device with no child, locks at once and tells the child a parent needs to check **Manage → Devices**.
- **Window:** the app reads `GET /api/v1/screen-time?target=personal` and takes this child's `unlockedUntil`. It converts that time to the device's clock using `serverTime`, so the device's own clock skew doesn't matter. The result is stored in the App Group.
- **Locking (fail closed):** `ScreenTimeLock.reconcile` shields the selection unless the stored window is still open. It runs on every sync, when the countdown hits zero, and in every monitor callback.
  - At each sync, the window is stored with a reading of the wall clock, `CLOCK_MONOTONIC` (time since boot, including sleep), the boot session ID and the time zone. Before the next sync, the window counts as locked if the wall clock and the monotonic clock disagree by more than 60 seconds (the clock was changed), if the device rebooted, or if the time zone changed. Otherwise the shorter of the two remainders counts. Going online and opening OpenCozy restores any genuine remaining time from the server.
  - The shield is lifted only while a schedule is in place that will lock it again. If `startMonitoring` throws, the stored window is cleared so the app and the extension agree it's locked; the next sync tries again.
  - The app never stops a running schedule to replace it. `startMonitoring` overwrites it, and Apple documents that stopping a running interval fires `intervalDidEnd`. An unchanged schedule isn't re-armed at all, to avoid Apple's "too tightly scheduled" error.
  - Nothing ever clears settings: neither the app nor the extension, even if the selection is missing, empty or unreadable. Existing shields stay in place. The App Group has no fallback to standard defaults; if it's misconfigured, Parent setup shows an error and the self-test fails.
- **Re-locking:** a one-off DeviceActivity schedule (`org.opencozy.grant`) runs from now to the window's end. Its `intervalDidEnd`, and `intervalWillEndWarning` for short windows, re-apply the shield in the monitor extension, even when the app isn't running. Three more layers cover a lost callback:
  - a second schedule (`org.opencozy.backstop`) ends a minute after the window (or after the 16-minute minimum) and reconciles again;
  - while the app is running, it reconciles and re-syncs a second after the window ends;
  - before skipping a re-arm, the app checks the system's own schedule (`DeviceActivityCenter.schedule(for:)`), not only what it stored, and re-arms if it is missing or different.
  - An end callback locks unless time genuinely remains. If more than 2 minutes remain (minute rounding, a window longer than one schedule), the extension arms a new schedule for the remainder. It never leaves the shield off with nothing scheduled. A callback within 10 seconds of arming comes from the schedule just replaced and is ignored while more than 20 seconds remain.
  - `intervalDidStart` only shields an expired window; it never arms. The schedule is recorded in the App Group before `startMonitoring`, because overwriting a schedule can fire the old one's callback in the extension while the app is still arming.
- **Why relocking has this many layers.** Relocking depends on callbacks the system can deliver late, twice or not at all, so no single path is trusted. The app runs its own end-of-window refresh as well as the monitor extension. A schedule is recorded before it is armed, so a callback from the schedule it overwrites can't arm it again. A re-arm is skipped only when the system itself still holds the matching schedule, not merely when the app's record says so.
  - An end callback within 10 seconds of arming comes from the schedule that arming just replaced, and the new schedule is kept. That stops the extension re-arming in a loop.
  - Schedule date components include `.timeZone`, so they name an absolute instant.
- **Blocked websites:** the screen-time response carries the household's `webFilter` lists (Manage → Devices → Blocked websites), stored in the App Group. Every reconcile applies them in a separate ManagedSettings store (`org.opencozy.web-filter`, `webContent.blockedByFilter = .specific(…)`), which covers every browser since all iOS browsers use WebKit, and works off the home network: game portals always, games only while the window is open (the reconcile's outcome is `unlocked`). Without a selection it fails closed. At most 50 domains are applied, portals first; Parent setup → Status shows the counts and any that didn't fit.
- **Offline:** if the server can't be reached, the last window the server confirmed is still honoured until it ends. No new time can be unlocked offline.

### Counting only real use (design, not built)

Today a spend opens a wall-clock window: 30 minutes is 30 minutes whether the child uses the iPad or puts it down. DeviceActivity can count real use instead, but that conflicts with the product rule that spent time is final and windows never move, so it needs a product decision first. This is the design if that rule changes.

- **What Apple offers.** A `DeviceActivitySchedule` can carry `DeviceActivityEvent`s: each names apps, categories and web domains (the locked selection) and a `threshold`, and the monitor extension's `eventDidReachThreshold` fires once the child's foreground use of them within the schedule's interval reaches it. The device counts this, offline, and the app never sees the usage itself.
- **Metered grants.** A spend would reserve a chunk (30 minutes) as a *metered* grant, not a window: no fixed end, only a latest end (the end of allowed hours, or midnight). The device lifts the shield and arms a schedule from now to that latest end, with events at 5, 10, … 30 minutes of use. Each event is recorded in the App Group and uploaded as a `screen_time_event` (`kind: "usage"`, seconds used so far), like **One more minute**. The last threshold shields at once in the extension. Reaching the latest end shields as today.
- **Server.** A metered grant debits the bank by reported use, not by the clock. When it closes (all used, the latest end, a new spend, or the device reporting the child locked it), the unused share goes back to the bank. Reports are monotonic per grant and capped at the reserved chunk, so a replay or a lost report never over-charges. Until a report arrives, the server treats the reserved time as spent, so a second device can't spend it as well.
- **Costs.** Thresholds count use only inside one schedule interval, and Apple's 15-minute minimum interval and limit of about 20 active schedules still apply. Events arrive with some delay (up to about a minute), and use of always-allowed apps never counts. Parents would see "used 12 of 30 min" instead of an end time, and the kid view's countdown would become a remaining-use meter that only the device knows.
- **Why not now.** It changes what a spend means: windows would move, and history would need corrections. It needs per-device reporting before the server can show the truth, and it can't be tested in the simulator. The wall-clock window stays until a parent asks for this.

### Notifications (child's device)

A paired child's device asks for notification permission at launch (and after pairing), registers its APNs token with the server (`PUT /api/v1/device/push-token`), and syncs whenever a push arrives. Pushes: a TV turned on and the child can start their own time, 5 minutes of TV time left, and a parent's answer to **Ask for more time** (which wakes the app to lift its shields).

### Communication apps are never locked

Messages, FaceTime, Phone and Contacts stay open even if the parent picks them, or picks a category that contains them (Messages and FaceTime are in Social). The picker flow says: "Messages and FaceTime are always allowed; set who they can contact in Screen Time → Communication Limits."

Family Controls tokens are opaque. In the app and the monitor extension, an app token can't be turned into a bundle identifier, and a token can't be made from one. So the app identifies these apps in two ways:

1. **Always allowed (guaranteed for the apps listed).** Under **Parent setup → Locked apps → Always allowed…**, the parent picks the apps in a second `FamilyActivityPicker`; the list shows their names and icons. Every time the shield is applied, those tokens are subtracted from the locked apps and passed as the `except:` set of the category shield (`ShieldSettings.ActivityCategoryPolicy.specific(_:except:)`). Category shields can exempt individual apps; there is no exemption API by bundle identifier.
2. **Detected (best effort).** When the system is about to shield an app, it hands the shield-configuration extension that app's `Application`, including its bundle identifier. If the app is Messages (`com.apple.MobileSMS`), FaceTime (`com.apple.facetime`), Phone (`com.apple.mobilephone`) or Contacts (`com.apple.MobileAddressBook`), the extension:
   - records the token in the App Group as always allowed, so every later shield leaves it out;
   - lifts that app's shield in the store straight away;
   - shows "Messages is always allowed. Close this and open Messages again."

   This needs the extension to share the App Group, and it only kicks in once the child has opened the app and met the shield. Apple doesn't document whether a shield-configuration extension may write to the App Group or the ManagedSettings store; if it can't, the shield tells the child to ask a parent to add the app under **Always allowed**.

## Known Apple limitations

- **Minimum interval of 15 minutes.** Apple documents [a 15-minute minimum interval](https://developer.apple.com/documentation/deviceactivity/deviceactivitycenter/monitoringerror/intervaltooshort) for DeviceActivity schedules (`intervalTooShort`). For a window under 16 minutes, the app stretches the schedule to 16 minutes and sets [`warningTime`](https://developer.apple.com/documentation/deviceactivity/deviceactivityschedule/warningtime) so that `intervalWillEndWarning` fires at the real end. `intervalDidEnd` is a backstop. `warningTime` is in whole minutes, so the schedule end is padded to keep the warning on the real end. This approach is suggested on Apple's developer forums. It still needs testing on a device.
- **Maximum of one week.** The forums report a one-week maximum interval (`intervalTooLong`). Plans are capped just below a week. Real windows are far shorter.
- **Callback timing.** The system delivers DeviceActivity callbacks, and they can be late, typically by seconds and occasionally longer. The forums also report occasional missed callbacks. The app re-checks whenever it's opened, but a missed callback while the app is closed leaves apps unlocked until OpenCozy is next opened.
- **No push from the server.** If an adult cancels a running screen-time redemption, the device re-locks at the next sync, when OpenCozy is opened or foregrounded, not instantly. A 401 or 403 locks at the next sync. The monitor extension doesn't call the API. Apple documents that the shield configuration extension can't make network requests, but says nothing either way for the monitor extension.
- **Clock and time-zone changes.** Clock jumps, reboots and time-zone changes are detected whenever the app or the extension runs, and they lock. Apple doesn't document whether DeviceActivity honours `.timeZone` in schedule components, so this needs a device check. If it doesn't and the clock is set back while the app is closed, the end callback can still arrive late. On a child account, Screen Time normally stops the child changing the date and time.
- **Opaque tokens.** The picker's selection is a set of opaque tokens that only work on this device. It's stored in the App Group, never sent to the server, and has to be chosen again on each device.
- **Opening OpenCozy from the shield.** `ShieldActionResponse.openParentalControlsApp` needs iOS 26.5. On older versions, the shield shows only **OK**.
- **Simulator.** The simulator can't do real child authorization or shielding. Authorization, shields and DeviceActivity callbacks have to be tested on a real device with a child account. Push notifications, passkeys (in the web view and natively) and the camera scanner also need a real device.

## Tests

`Tests/Support/FixtureAPI.swift` holds fictional data for the tests. `AppModel(preview:)` skips every Family Controls call. Parent-mode tests inject a fake `ParentAPI`. The web view's rules (`WebPolicy`, `ShellMessage`, `KidWebSession`) are plain types tested without WebKit.
