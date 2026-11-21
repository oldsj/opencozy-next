import CryptoKit
import Foundation
import Testing
@testable import OpenCozy

// Phase 2.5: the app is a web view around the web app. Navigation stays on the
// canonical origin, the bridge is one message, and the kid device signs its web
// view in with a signed one-time exchange.

struct WebPolicyTests {
    let origin = URL(string: "https://opencozy.example.org")!

    @Test func staysOnTheCanonicalOrigin() {
        #expect(WebPolicy.decide(URL(string: "https://opencozy.example.org/today"), origin: origin, isMainFrame: true) == .allow)
        #expect(WebPolicy.decide(URL(string: "https://OpenCozy.Example.org:443/manage"), origin: origin, isMainFrame: true) == .allow)
        #expect(WebPolicy.decide(URL(string: "about:blank"), origin: origin, isMainFrame: true) == .allow)
    }

    @Test func otherOriginsOpenInSafari() {
        #expect(WebPolicy.decide(URL(string: "https://example.com/help"), origin: origin, isMainFrame: true) == .openExternally)
        // Same host, other scheme or port, is another origin.
        #expect(WebPolicy.decide(URL(string: "http://opencozy.example.org/today"), origin: origin, isMainFrame: true) == .openExternally)
        #expect(WebPolicy.decide(URL(string: "https://opencozy.example.org:8443/"), origin: origin, isMainFrame: true) == .openExternally)
        #expect(WebPolicy.decide(URL(string: "https://opencozy.example.org.evil.example/"), origin: origin, isMainFrame: true) == .openExternally)
        #expect(WebPolicy.decide(URL(string: "mailto:parent@example.org"), origin: origin, isMainFrame: true) == .openExternally)
    }

    @Test func embeddedFramesMayLoadButNotOddSchemes() {
        #expect(WebPolicy.decide(URL(string: "https://maps.example.com/embed"), origin: origin, isMainFrame: false) == .cancel)
        #expect(WebPolicy.decide(URL(string: "https://opencozy.example.org/frame"), origin: origin, isMainFrame: false) == .allow)
        #expect(WebPolicy.decide(URL(string: "tel:123"), origin: origin, isMainFrame: false) == .cancel)
        #expect(WebPolicy.decide(URL(string: "javascript:alert(1)"), origin: origin, isMainFrame: true) == .cancel)
        #expect(WebPolicy.decide(URL(string: "file:///etc/hosts"), origin: origin, isMainFrame: true) == .cancel)
        #expect(WebPolicy.decide(URL(string: "opencozy-pair:v1?x"), origin: origin, isMainFrame: true) == .cancel)
        #expect(WebPolicy.decide(nil, origin: origin, isMainFrame: true) == .cancel)
    }

    @Test func sessionEndedPagesAreRecognisedOnlyOnTheOrigin() {
        #expect(WebPolicy.isSessionEndedPage(URL(string: "https://opencozy.example.org/device/expired"), origin: origin))
        #expect(WebPolicy.isSessionEndedPage(URL(string: "https://opencozy.example.org/login?next=%2Fkid"), origin: origin))
        #expect(!WebPolicy.isSessionEndedPage(URL(string: "https://opencozy.example.org/loginhelp"), origin: origin))
        #expect(!WebPolicy.isSessionEndedPage(URL(string: "https://opencozy.example.org/kid"), origin: origin))
        #expect(!WebPolicy.isSessionEndedPage(URL(string: "https://example.com/device/expired"), origin: origin))
    }

    @Test func cancellationsAreNotOffline() {
        #expect(WebShell.isCancellation(NSError(domain: NSURLErrorDomain, code: NSURLErrorCancelled)))
        #expect(WebShell.isCancellation(NSError(domain: "WebKitErrorDomain", code: 102)))
        #expect(!WebShell.isCancellation(NSError(domain: NSURLErrorDomain, code: NSURLErrorCannotConnectToHost)))
    }
}

struct ShellMessageTests {
    @Test func onlyScreenTimeChangedCrossesTheBridge() {
        #expect(ShellMessage.handlerName == "opencozy")
        #expect(ShellMessage(body: ["type": "screenTimeChanged"]) == .screenTimeChanged)
        #expect(ShellMessage(body: ["type": "openSettings"]) == nil)
        #expect(ShellMessage(body: "screenTimeChanged") == nil)
        #expect(ShellMessage(body: ["kind": "screenTimeChanged"]) == nil)
    }
}

@MainActor
struct WebSessionTests {
    static func config(origin: String = "https://opencozy.example.org") -> AppConfig {
        let unique = "org.opencozy.tests.\(UUID().uuidString)"
        let config = AppConfig(keys: DeviceKeyStore(keychain: Keychain(service: unique)), clock: DeviceClock(defaultsSuite: unique), defaultsSuite: unique)
        config.baseURL = URL(string: origin)
        return config
    }

    @Test func exchangeURLMustBeOnThePairedOrigin() {
        let origin = URL(string: "https://opencozy.example.org")!
        #expect(DeviceWebSession.resolve("/device/session?token=abc", origin: origin)?.absoluteString
            == "https://opencozy.example.org/device/session?token=abc")
        #expect(DeviceWebSession.resolve("https://opencozy.example.org/device/session?token=abc", origin: origin) != nil)
        #expect(DeviceWebSession.resolve("https://example.com/device/session?token=abc", origin: origin) == nil)
        #expect(DeviceWebSession.resolve("//example.com/device/session", origin: origin) == nil)
        #expect(DeviceWebSession.resolve("http://opencozy.example.org/device/session", origin: origin) == nil)
        #expect(DeviceWebSession.resolve("javascript:alert(1)", origin: origin) == nil)
        // Only the exchange path: never a same-origin page of the server's choosing.
        #expect(DeviceWebSession.resolve("https://opencozy.example.org/manage/devices", origin: origin) == nil)
        #expect(DeviceWebSession.resolve("https://opencozy.example.org/device/session/x?token=abc", origin: origin) == nil)
    }

    @Test func decodesTheResponse() throws {
        // The server sends an absolute URL on its canonical origin, with 201.
        let json = #"{ "url": "https://opencozy.example.org/device/session?token=abc", "expiresAt": "2026-11-07T10:01:00.000Z" }"#
        let session = try APIDates.decoder().decode(DeviceWebSession.self, from: Data(json.utf8))
        #expect(session.url == "https://opencozy.example.org/device/session?token=abc")
        #expect(session.expiresAt != nil)
    }

    @Test func modelResolvesTheExchangeURL() async throws {
        let model = AppModel(preview: FixtureAPI(), config: Self.config())
        let url = try await model.webSessionURL()
        #expect(url.absoluteString == "https://opencozy.example.org/device/session?token=example-one-time-token")
    }

    @Test func aForeignExchangeURLIsRefused() async {
        struct ForeignAPI: OpenCozyAPI {
            func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
            func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) { try await FixtureAPI().screenTime(childId: childId) }
            func webSession() async throws -> DeviceWebSession { DeviceWebSession(url: "https://example.com/steal") }
        }
        let model = AppModel(preview: ForeignAPI(), config: Self.config())
        await #expect(throws: APIError.self) { try await model.webSessionURL() }
        #expect(model.accessProblem == nil)
    }

    /// The exchange is a signed `POST /api/device/web-session` with a `{}` body.
    @Test func exchangeIsASignedPost() async throws {
        let key = SoftwareDeviceKey(key: P256.Signing.PrivateKey())
        let signer = DeviceRequestSigner(deviceId: "D1", key: key, clock: DeviceClock(defaultsSuite: "org.opencozy.tests.\(UUID().uuidString)"))
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CapturingProtocol.self]
        let api = HTTPOpenCozyAPI(baseURL: URL(string: "https://opencozy.example.org")!, signer: signer, session: URLSession(configuration: configuration))
        let session = try await api.webSession()
        #expect(session.url == "https://opencozy.example.org/device/session?token=abc")

        let request = try #require(CapturingProtocol.last)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path() == "/api/device/web-session")
        #expect(request.value(forHTTPHeaderField: "X-OpenCozy-Client")?.hasPrefix("OpenCozy-iOS/") == true)
        #expect(request.value(forHTTPHeaderField: "Authorization") == nil)
        #expect(request.value(forHTTPHeaderField: "X-OpenCozy-Device") == "d1")
        let body = CapturingProtocol.lastBody ?? Data()
        #expect(String(decoding: body, as: UTF8.self) == "{}")
        let timestamp = try #require(request.value(forHTTPHeaderField: "X-OpenCozy-Timestamp").flatMap(Int.init))
        let nonce = try #require(request.value(forHTTPHeaderField: "X-OpenCozy-Nonce"))
        let message = DeviceRequestSigner.canonical(method: "POST", pathAndQuery: "/api/device/web-session", timestamp: timestamp,
                                                    nonce: nonce, body: body, deviceId: "D1")
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: try #require(
            request.value(forHTTPHeaderField: "X-OpenCozy-Signature").flatMap { Data(base64URL: $0) }))
        #expect(key.key.publicKey.isValidSignature(signature, for: Data(message.utf8)))
    }
}

/// Answers every request with a fixed web-session response and keeps the last one.
final class CapturingProtocol: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var last: URLRequest?
    nonisolated(unsafe) static var lastBody: Data?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.last = request
        Self.lastBody = request.httpBody ?? request.httpBodyStream.map(Self.read)
        let body = Data(#"{ "url": "https://opencozy.example.org/device/session?token=abc", "expiresAt": "2026-11-07T10:01:00.000Z" }"#.utf8)
        let response = HTTPURLResponse(url: request.url!, statusCode: 201, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func read(_ stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

@MainActor
struct KidWebSessionTests {
    final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }
    final class Log { var loaded: [URL] = []; var exchanges = 0 }

    private func session(clock: Clock = Clock(), log: Log, result: @escaping () throws -> URL) -> KidWebSession {
        KidWebSession(now: { clock.now }, exchange: {
            log.exchanges += 1
            return try result()
        }, load: { log.loaded.append($0) })
    }

    private let exchangeURL = URL(string: "https://opencozy.example.org/device/session?token=abc")!

    @Test func signsInByLoadingTheExchangeURL() async {
        let log = Log()
        let session = session(log: log) { exchangeURL }
        await session.signIn()
        #expect(session.state == .signedIn)
        #expect(log.loaded == [exchangeURL])
    }

    @Test func anEndedSessionGetsANewExchange() async {
        let clock = Clock()
        let log = Log()
        let session = session(clock: clock, log: log) { exchangeURL }
        await session.signIn()
        clock.now += 30 * 60
        await session.signIn() // the web view reached /login
        #expect(log.exchanges == 2)
        #expect(session.state == .signedIn)
    }

    @Test func aServerThatKeepsSigningOutStopsTheLoop() async {
        let clock = Clock()
        let log = Log()
        let session = session(clock: clock, log: log) { exchangeURL }
        for _ in 0..<5 {
            await session.signIn()
            clock.now += 5
        }
        #expect(log.exchanges == KidWebSession.maxExchanges)
        #expect(session.state == .failed(KidWebSession.loopMessage))
        // Try again (a person tapping) is allowed.
        await session.signIn(userInitiated: true)
        #expect(session.state == .signedIn)
        #expect(log.exchanges == KidWebSession.maxExchanges + 1)
    }

    @Test func offlineAndMissingEndpointAreExplained() async {
        let offline = session(log: Log()) { throw APIError.transport("offline") }
        await offline.signIn()
        #expect(offline.state == .unreachable)

        let old = session(log: Log()) { throw APIError.server(status: 404, message: "Not found.") }
        await old.signIn()
        #expect(old.state == .failed(KidWebSession.unsupportedMessage))
    }
}

@MainActor
struct ParentShellTests {
    private func model() -> ParentModel {
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        return ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: FakeParentServer(approvals: []), token: t) }, notifier: .none,
                           webData: WebDataGate(defaults: UserDefaults(suiteName: "org.opencozy.tests.\(unique)")!, clear: {}))
    }

    @Test func connectNeedsAnHTTPSAddress() {
        let model = model()
        #expect(!model.connect("http://192.0.2.10:3000"))
        #expect(model.origin == nil)
        #expect(model.connect("https://opencozy.example.org/manage"))
        #expect(model.origin == URL(string: "https://opencozy.example.org"))
        #expect(model.store.baseURL == model.origin)
    }

    @Test func notificationsSignInUsesTheConnectedServer() async {
        let model = model()
        model.connect("https://opencozy.example.org")
        #expect(await model.signIn(server: model.origin!.absoluteString, email: "parent@example.org", password: "correct horse", deviceName: "Phone"))
        #expect(model.isSignedIn)
        // Another server drops this phone's notification session.
        model.connect("https://other.example.org")
        #expect(!model.isSignedIn)
    }

    @Test func disconnectForgetsTheServerAndSession() async {
        let model = model()
        model.connect("https://opencozy.example.org")
        _ = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Phone")
        await model.disconnect()
        #expect(model.origin == nil)
        #expect(!model.isSignedIn)
    }
}

struct WebSignInWatcherTests {
    /// Feeds URLs through one watcher and returns which ones trigger a try.
    private func run(_ urls: [String?]) -> [Bool] {
        var watcher = WebSignInWatcher(origin: URL(string: "https://opencozy.example.org")!)
        return urls.map { text in watcher.shouldTry(after: text.flatMap(URL.init(string:))) }
    }

    @Test func triesOnTheFirstPageAndAfterLeavingSignIn() {
        let results = run(["https://opencozy.example.org/manage/inbox", "https://opencozy.example.org/manage/chores",
                           "https://opencozy.example.org/login",
                           // The web sign-in finished (router.push from /login).
                           "https://opencozy.example.org/manage/displays", "https://opencozy.example.org/today"])
        #expect(results == [true, false, false, true, false])
    }

    @Test func startingOnSignInWaitsForItToFinish() {
        #expect(run(["https://opencozy.example.org/login", "https://opencozy.example.org/manage/displays"]) == [false, true])
    }

    @Test func ignoresOtherOriginsAndNil() {
        #expect(run([nil, "https://example.com/manage"]) == [false, false])
    }
}

@MainActor
struct WebHandoffTests {
    private func model() -> ParentModel {
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: FakeParentServer(approvals: []), token: t) }, notifier: .none,
                                webData: WebDataGate(defaults: UserDefaults(suiteName: "org.opencozy.tests.\(unique)")!, clear: {}))
        model.connect("https://opencozy.example.org")
        return model
    }

    private static let minted = Data(#"""
    { "token": "ocp_from_web",
      "session": { "id": "s2", "deviceName": "Example phone", "createdAt": "2026-11-07T10:00:00.000Z", "expiresAt": "2027-02-05T10:00:00.000Z", "lastUsedAt": null },
      "household": { "id": "h", "name": "The Example House" } }
    """#.utf8)

    @Test func mintsTheNotificationSessionFromTheWebCookie() async {
        let model = model()
        var sent: String?
        let result = await model.enableFromWebSession(deviceName: "  Example phone  ") { name in
            sent = name
            return (201, Self.minted)
        }
        #expect(result == .enabled)
        #expect(sent == "Example phone")
        #expect(model.isSignedIn)
        #expect(model.store.token == "ocp_from_web")
        #expect(model.household?.name == "The Example House")
    }

    @Test(arguments: [(401, ParentModel.WebHandoff.notSignedIn), (403, .notSignedIn), (404, .unsupported), (405, .unsupported), (500, .failed)])
    func failuresStayQuiet(status: Int, expected: ParentModel.WebHandoff) async {
        let model = model()
        let result = await model.enableFromWebSession(deviceName: "Phone") { _ in (status, Data(#"{"error":"Unauthorized."}"#.utf8)) }
        #expect(result == expected)
        #expect(!model.isSignedIn)
        #expect(model.errorMessage == nil)
    }

    @Test func transportFailureStaysQuiet() async {
        let model = model()
        let result = await model.enableFromWebSession(deviceName: "Phone") { _ in throw APIError.transport("offline") }
        #expect(result == .failed)
        #expect(!model.isSignedIn)
    }

    @Test func noWebCookieMeansNotSignedIn() async {
        let model = model()
        #expect(await model.enableFromWebSession(deviceName: "Phone") { _ in nil } == .notSignedIn)
        #expect(!model.isSignedIn)
    }

    @Test func skipsWhenAlreadyOn() async {
        let model = model()
        _ = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Phone")
        var called = false
        let result = await model.enableFromWebSession(deviceName: "Phone") { _ in called = true; return (201, Self.minted) }
        #expect(result == .skipped)
        #expect(!called)
        #expect(model.store.token == "ocp_test")
    }

    @Test func stopNotificationsKeepsThemOffUntilManualSignIn() async {
        let model = model()
        _ = await model.enableFromWebSession(deviceName: "Phone") { _ in (201, Self.minted) }
        await model.stopNotifications()
        #expect(!model.isSignedIn)
        var called = false
        let again = await model.enableFromWebSession(deviceName: "Phone") { _ in called = true; return (201, Self.minted) }
        #expect(again == .skipped)
        #expect(!called)
        // Signing in from the bell sheet turns the automatic path back on.
        _ = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Phone")
        #expect(!model.store.webHandoffOff)
    }

    @Test func noServerNoCall() async {
        let model = model()
        await model.disconnect()
        var called = false
        let again = await model.enableFromWebSession(deviceName: "Phone") { _ in called = true; return (201, Self.minted) }
        #expect(again == .skipped)
        #expect(!called)
        // Changing server isn't the parent turning notifications off.
        #expect(!model.store.webHandoffOff)
    }
}

@MainActor
struct WebDataGateTests {
    private final class Counter { var clears = 0 }

    private func gate() -> (WebDataGate, Counter) {
        let counter = Counter()
        let gate = WebDataGate(defaults: UserDefaults(suiteName: "org.opencozy.tests.\(UUID().uuidString)")!, clear: { counter.clears += 1 })
        return (gate, counter)
    }

    let origin = URL(string: "https://opencozy.example.org")!

    @Test func wipesWhenTheOwnerChanges() async {
        let (gate, counter) = gate()
        await gate.prepare(for: .parent(origin: origin))
        #expect(counter.clears == 1)
        // Same owner again (relaunch): keep the cookies.
        await gate.prepare(for: .parent(origin: origin))
        #expect(counter.clears == 1)
        // Parent → kid on the same server: the adult cookie must go.
        await gate.prepare(for: .kid(deviceId: "D1", origin: origin))
        #expect(counter.clears == 2)
        // Re-paired as another device.
        await gate.prepare(for: .kid(deviceId: "D2", origin: origin))
        #expect(counter.clears == 3)
        // Another server.
        await gate.prepare(for: .kid(deviceId: "D2", origin: URL(string: "https://other.example.org")!))
        #expect(counter.clears == 4)
    }

    @Test func resetWipesNowAndBeforeTheNextLoad() async {
        let (gate, counter) = gate()
        await gate.prepare(for: .kid(deviceId: "D1", origin: origin))
        gate.reset()
        #expect(gate.owner == nil)
        // Even the same owner is wiped again after a reset (lock, unpair, role switch).
        await gate.prepare(for: .kid(deviceId: "D1", origin: origin))
        #expect(counter.clears == 3)
        #expect(gate.owner == WebDataGate.Owner.kid(deviceId: "D1", origin: origin).key)
    }

    @Test func operationsRunInCallOrder() async {
        let log = LogBox()
        let gate = WebDataGate(defaults: UserDefaults(suiteName: "org.opencozy.tests.\(UUID().uuidString)")!) {
            log.entries.append("clear-start")
            await Task.yield()
            log.entries.append("clear-end")
        }
        gate.reset()
        await gate.prepare(for: .parent(origin: origin))
        log.entries.append("loaded")
        #expect(log.entries == ["clear-start", "clear-end", "clear-start", "clear-end", "loaded"])
    }

    private final class LogBox { var entries: [String] = [] }
}

struct WebCookiesTests {
    let url = URL(string: "https://opencozy.example.org/api/parent-sessions")!

    private func cookie(_ name: String, domain: String = "opencozy.example.org", path: String = "/", secure: Bool = true,
                        expires: Date? = nil) -> HTTPCookie {
        var properties: [HTTPCookiePropertyKey: Any] = [.name: name, .value: "v-\(name)", .domain: domain, .path: path]
        if secure { properties[.secure] = "TRUE" }
        if let expires { properties[.expires] = expires }
        return HTTPCookie(properties: properties)!
    }

    @Test func sendsOnlyWhatABrowserWould() {
        let cookies = [cookie("__Secure-better-auth.session_token"), cookie("other-host", domain: "example.com"),
                       cookie("old", expires: Date(timeIntervalSinceNow: -60)), cookie("deep", path: "/manage"),
                       cookie("parent-domain", domain: ".example.org")]
        let names = Set(WebCookies.matching(cookies, url: url).map(\.name))
        #expect(names == ["__Secure-better-auth.session_token", "parent-domain"])
        #expect(WebCookies.matching([cookie("s")], url: URL(string: "http://opencozy.example.org/api/parent-sessions")!).isEmpty)
    }

    @Test func refusesWhenADeviceCookieIsPresent() {
        #expect(WebCookies.parentHeader([cookie("__Secure-better-auth.session_token"), cookie("__Host-opencozy-device")], url: url) == nil)
        #expect(WebCookies.parentHeader([cookie("better-auth.session_token", secure: false), cookie("opencozy-device", secure: false)],
                                        url: URL(string: "http://localhost:8765/api/parent-sessions")!.replacingHost("localhost")) == nil)
        #expect(WebCookies.parentHeader([], url: url) == nil)
        #expect(WebCookies.parentHeader([cookie("__Secure-better-auth.session_token")], url: url)
                == "__Secure-better-auth.session_token=v-__Secure-better-auth.session_token")
    }

    @Test func mintRequestIsNativeWithCookieAndOrigin() throws {
        let request = try ParentSessionMint.request(origin: URL(string: "https://opencozy.example.org")!, cookie: "a=b", deviceName: "Example phone")
        #expect(request.httpMethod == "POST")
        #expect(request.url?.absoluteString == "https://opencozy.example.org/api/parent-sessions")
        #expect(request.value(forHTTPHeaderField: "Cookie") == "a=b")
        #expect(request.value(forHTTPHeaderField: "Origin") == "https://opencozy.example.org")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        #expect(!request.httpShouldHandleCookies)
        // The server refuses any Sec-Fetch-Site other than none: a native request sends no Sec-Fetch headers.
        #expect((request.allHTTPHeaderFields ?? [:]).keys.allSatisfy { !$0.lowercased().hasPrefix("sec-fetch") })
        let body = try JSONDecoder().decode([String: String].self, from: try #require(request.httpBody))
        #expect(body == ["deviceName": "Example phone"])
        #expect(ParentSessionMint.originHeader(URL(string: "http://localhost:8765")!) == "http://localhost:8765")
        #expect(ParentSessionMint.originHeader(URL(string: "https://OpenCozy.example.org:443/")!) == "https://opencozy.example.org")
    }
}

private extension URL {
    func replacingHost(_ host: String) -> URL {
        var components = URLComponents(url: self, resolvingAgainstBaseURL: false)!
        components.host = host
        return components.url!
    }
}

struct DeviceNameTests {
    let now = ISO8601DateFormatter().date(from: "2026-11-07T10:00:00Z")!

    @Test func keepsAUserAssignedName() {
        #expect(DeviceName.suggested(name: "Example's iPhone", model: "iPhone", machine: "iPhone17,3", now: now) == "Example's iPhone")
    }

    @Test func genericNameGetsHardwareAndDate() {
        let name = DeviceName.suggested(name: "iPhone", model: "iPhone", machine: "iPhone17,3", now: now)
        #expect(name.hasPrefix("iPhone (iPhone17,3), "))
        #expect(name.contains("2026"))
        #expect(name != "iPhone")
        #expect(DeviceName.suggested(name: " ", model: "iPad", machine: "", now: now).hasPrefix("iPad, "))
    }

    @Test func machineIdentifierIsNotEmpty() {
        #expect(!DeviceName.machineIdentifier().isEmpty)
    }
}
