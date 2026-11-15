import CryptoKit
import Foundation
import Testing
@testable import OpenCozy

// Fixes from the Phase 2 review: push environment (S2), the Phase-1 key (S6),
// resolved pushes and stale actions (S8), https-only origins (N16), and strict
// query encoding for signed requests (N17).

struct PushEnvironmentTests {
    /// A provisioning profile is a CMS envelope around an XML plist; only the plist
    /// part matters, so binary padding stands in for the signature.
    static func profile(aps: String?) -> Data {
        let entitlements = aps.map { "<key>aps-environment</key><string>\($0)</string>" } ?? ""
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0"><dict><key>Name</key><string>Example</string>
        <key>Entitlements</key><dict>\(entitlements)<key>get-task-allow</key><true/></dict></dict></plist>
        """
        return Data([0x30, 0x82, 0x0f, 0x00, 0x06, 0x09]) + Data(plist.utf8) + Data([0xa0, 0x82, 0x00, 0x00])
    }

    @Test func noProfileMeansAppStoreOrTestFlight() {
        #expect(PushEnvironment.from(provisioningProfile: nil) == .production)
    }

    @Test func readsTheEntitlementFromTheProfile() {
        #expect(PushEnvironment.from(provisioningProfile: Self.profile(aps: "development")) == .development)
        #expect(PushEnvironment.from(provisioningProfile: Self.profile(aps: "production")) == .production)
    }

    @Test func unreadableProfileIsDevelopment() {
        #expect(PushEnvironment.from(provisioningProfile: Self.profile(aps: nil)) == .development)
        #expect(PushEnvironment.from(provisioningProfile: Data([0x00, 0x01, 0x02])) == .development)
    }
}

@MainActor
struct LegacyCredentialTests {
    private func isolatedConfig() -> (AppConfig, Keychain, UserDefaults) {
        let unique = "org.opencozy.tests.\(UUID().uuidString)"
        let keychain = Keychain(service: unique)
        let config = AppConfig(keys: DeviceKeyStore(keychain: keychain), clock: DeviceClock(defaultsSuite: unique), defaultsSuite: unique)
        return (config, keychain, UserDefaults(suiteName: unique)!)
    }

    @Test func purgeDeletesThePhase1KeyAndURL() {
        let (config, keychain, defaults) = isolatedConfig()
        keychain.set("ocz_example_not_a_real_key", for: AppConfig.legacyTokenAccount)
        defaults.set("https://opencozy.example.org", forKey: AppConfig.legacyBaseURLKey)
        config.purgeLegacyCredential()
        #expect(keychain.string(for: AppConfig.legacyTokenAccount) == nil)
        #expect(defaults.string(forKey: AppConfig.legacyBaseURLKey) == nil)
        config.purgeLegacyCredential() // idempotent
    }

    @Test func pairingDeletesThePhase1Key() async throws {
        let (config, keychain, _) = isolatedConfig()
        keychain.set("ocz_example_not_a_real_key", for: AppConfig.legacyTokenAccount)
        let model = AppModel(preview: FixtureAPI(), config: config)
        _ = try await model.pair(.code(origin: URL(string: "https://opencozy.example.org")!, code: "ABCD-EFGH"), deviceName: "Tablet",
                                 pairing: PairingFlowTests.FakePairing(recorder: PairingFlowTests.Recorder()))
        #expect(keychain.string(for: AppConfig.legacyTokenAccount) == nil)
        #expect(config.signer != nil)
    }
}

@MainActor
final class NotificationLog {
    var removed: [String] = []
    var badges: [Int] = []
    var posted: [(title: String, body: String)] = []

    var notifier: ApprovalNotifier {
        ApprovalNotifier(sync: { _, _ in },
                         remove: { [self] in removed.append($0) },
                         setBadge: { [self] in badges.append($0) },
                         post: { [self] in posted.append(($0, $1)) })
    }
}

@MainActor
struct ResolvedPushTests {
    private func signedIn(_ approvals: [InboxItem], log: NotificationLog) async -> (ParentModel, FakeParentServer) {
        let server = FakeParentServer(approvals: approvals)
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: log.notifier)
        _ = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Test phone")
        return (model, server)
    }

    @Test func resolvedPushRemovesItsNotificationAndSetsTheBadge() async {
        let log = NotificationLog()
        let (model, _) = await signedIn([ParentModelTests.approval("a", minutesAgo: 9), ParentModelTests.approval("b", minutesAgo: 5)], log: log)
        await model.handleResolvedPush(ApprovalPush(approvalId: "a", event: .resolved, pendingCount: 1))
        #expect(log.removed == ["a"])
        #expect(log.badges == [1])
        #expect(model.approvals.map(\.id) == ["b"])
        #expect(model.pendingCount == 1)
    }

    @Test func resolvedPushWorksSignedOutAndWithoutACount() async {
        let log = NotificationLog()
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, _ in FakeParentAPI(server: FakeParentServer(approvals: []), token: nil) }, notifier: log.notifier)
        await model.handleResolvedPush(ApprovalPush(approvalId: "a", event: .resolved))
        #expect(log.removed == ["a"])
        #expect(log.badges.isEmpty)
    }

    @Test func pendingPushIsNotTreatedAsResolved() async {
        let log = NotificationLog()
        let (model, _) = await signedIn([ParentModelTests.approval("a", minutesAgo: 5)], log: log)
        await model.handleResolvedPush(ApprovalPush(approvalId: "a", event: .pending(version: 1), pendingCount: 0))
        #expect(log.removed.isEmpty)
        #expect(model.approvals.map(\.id) == ["a"])
    }

    @Test func staleActionSaysAlreadyHandledAndClearsTheNotification() async {
        let log = NotificationLog()
        let (model, server) = await signedIn([ParentModelTests.approval("a", minutesAgo: 5)], log: log)
        await server.fail([.server(status: 409, message: "Someone already decided this one.")])
        let outcome = await model.handleAction(.reject, push: ApprovalPush(approvalId: "a", event: .pending(version: 1)))
        #expect(outcome == .alreadyHandled)
        #expect(log.removed == ["a"])
        #expect(log.posted.map(\.title) == [ParentModel.alreadyHandledTitle])
        #expect(log.posted.first?.body == "Someone already decided this one.")
    }

    @Test func successfulActionClearsTheNotificationQuietly() async {
        let log = NotificationLog()
        let (model, _) = await signedIn([ParentModelTests.approval("a", minutesAgo: 5)], log: log)
        let outcome = await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending(version: 1)))
        #expect(outcome == .done)
        #expect(log.removed == ["a"])
        #expect(log.posted.isEmpty)
    }

    @Test func unreachableActionKeepsTheNotificationAndSaysWhy() async {
        let log = NotificationLog()
        let (model, server) = await signedIn([ParentModelTests.approval("a", minutesAgo: 5)], log: log)
        await server.fail([.transport("offline"), .transport("offline"), .transport("offline")])
        let outcome = await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending(version: 1)))
        #expect(outcome == .failed)
        #expect(log.removed.isEmpty)
        #expect(log.posted.map(\.title) == [ParentModel.notSentTitle])
        #expect(log.posted.first?.body == ParentModel.unreachableMessage)
    }
}

struct OriginTests {
    @Test func httpsOnlyExceptThisMachine() {
        #expect(BaseURL.parse("https://opencozy.example.org")?.absoluteString == "https://opencozy.example.org")
        #expect(BaseURL.parse("http://opencozy.example.org") == nil)
        #expect(BaseURL.parse("http://192.0.2.10:3000") == nil)
        #expect(BaseURL.parse("http://localhost:3000")?.absoluteString == "http://localhost:3000")
        #expect(BaseURL.parse("http://127.0.0.1:3000")?.absoluteString == "http://127.0.0.1:3000")
    }
}

struct StrictQueryTests {
    @Test func encodesEverythingOutsideUnreserved() throws {
        var components = URLComponents(string: "https://opencozy.example.org/api/v1/x")!
        components.queryItems = [URLQueryItem(name: "q", value: "it's a café"), URLQueryItem(name: "target", value: "personal")]
        let url = try #require(components.url)
        let strict = DeviceRequestSigner.strictQuery(url)
        #expect(DeviceRequestSigner.pathAndQuery(of: strict) == "/api/v1/x?q=it%27s%20a%20caf%C3%A9&target=personal")
        // Simple queries and existing escapes are left alone.
        let simple = URL(string: "https://opencozy.example.org/api/v1/screen-time?target=personal")!
        #expect(DeviceRequestSigner.strictQuery(simple) == simple)
        let escaped = URL(string: "https://opencozy.example.org/x?q=a%20b")!
        #expect(DeviceRequestSigner.strictQuery(escaped) == escaped)
    }

    @Test func theRequestSentIsTheOneSigned() throws {
        let key = SoftwareDeviceKey(key: P256.Signing.PrivateKey())
        let signer = DeviceRequestSigner(deviceId: "d", key: key, clock: DeviceClock(defaultsSuite: "org.opencozy.tests.\(UUID().uuidString)"))
        var components = URLComponents(string: "https://opencozy.example.org/api/v1/x")!
        components.queryItems = [URLQueryItem(name: "q", value: "it's")]
        var request = URLRequest(url: try #require(components.url))
        try signer.sign(&request, nonce: "nonce-nonce-nonce-1")
        let sent = try #require(request.url)
        #expect(sent.query(percentEncoded: true) == "q=it%27s")
        let timestamp = try #require(request.value(forHTTPHeaderField: "X-OpenCozy-Timestamp").flatMap(Int.init))
        let message = DeviceRequestSigner.canonical(method: "GET", pathAndQuery: "/api/v1/x?q=it%27s", timestamp: timestamp,
                                                    nonce: "nonce-nonce-nonce-1", body: Data(), deviceId: "d")
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: try #require(
            request.value(forHTTPHeaderField: "X-OpenCozy-Signature").flatMap { Data(base64URL: $0) }))
        #expect(key.key.publicKey.isValidSignature(signature, for: Data(message.utf8)))
    }
}
