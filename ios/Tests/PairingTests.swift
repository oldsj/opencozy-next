import CryptoKit
import Foundation
import Testing
@testable import OpenCozy

// Contract "Phase 2 server API, part 2": pairing, signed requests, passkeys.

struct SignedRequestTests {
    static let deviceId = "0f3c2a1e-5b6d-4e7f-8a9b-0c1d2e3f4a5b"

    @Test func canonicalStringMatchesTheContract() {
        let string = DeviceRequestSigner.canonical(method: "get", pathAndQuery: "/api/v1/screen-time?target=personal",
                                                   timestamp: 1_800_000_000, nonce: "abcdefghijklmnop", body: Data(),
                                                   deviceId: Self.deviceId.uppercased())
        #expect(string == """
        OPENCOZY-DEVICE-V1
        GET
        /api/v1/screen-time?target=personal
        1800000000
        abcdefghijklmnop
        e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
        \(Self.deviceId)
        """)
        #expect(!string.hasSuffix("\n"))
    }

    @Test func bodyHashIsOverExactBytes() {
        let body = Data(#"{"a":1}"#.utf8)
        let line = DeviceRequestSigner.canonical(method: "POST", pathAndQuery: "/x", timestamp: 1, nonce: "n", body: body, deviceId: "d")
            .split(separator: "\n")[5]
        #expect(line == SHA256.hash(data: body).map { String(format: "%02x", $0) }.joined())
    }

    @Test func pathAndQueryIsAsSent() throws {
        var url = URL(string: "https://opencozy.example.org")!.appending(path: "api/v1/screen-time")
        url.append(queryItems: [URLQueryItem(name: "target", value: "personal")])
        #expect(DeviceRequestSigner.pathAndQuery(of: url) == "/api/v1/screen-time?target=personal")
        let step = URL(string: "https://opencozy.example.org")!.appending(path: "api/v1/routines/occurrences/o1/steps/s1")
        #expect(DeviceRequestSigner.pathAndQuery(of: step) == "/api/v1/routines/occurrences/o1/steps/s1")
    }

    @Test func signedHeadersVerifyWithThePublicKey() throws {
        let key = SoftwareDeviceKey(key: P256.Signing.PrivateKey())
        let suite = "org.opencozy.tests.\(UUID().uuidString)"
        let signer = DeviceRequestSigner(deviceId: Self.deviceId, key: key, clock: DeviceClock(defaultsSuite: suite))
        var request = URLRequest(url: URL(string: "https://opencozy.example.org/api/v1/rewards/r1/redeem")!)
        request.httpMethod = "POST"
        request.httpBody = Data(#"{"requestId":"x"}"#.utf8)
        request.setValue("Bearer leftover", forHTTPHeaderField: "Authorization")
        try signer.sign(&request, nonce: "nonce-nonce-nonce-1")

        #expect(request.value(forHTTPHeaderField: "Authorization") == nil)
        #expect(request.value(forHTTPHeaderField: "X-OpenCozy-Device") == Self.deviceId)
        let timestamp = try #require(request.value(forHTTPHeaderField: "X-OpenCozy-Timestamp").flatMap(Int.init))
        #expect(abs(Double(timestamp) - Date().timeIntervalSince1970) < 5)
        let signatureText = try #require(request.value(forHTTPHeaderField: "X-OpenCozy-Signature"))
        #expect(signatureText.count == 86) // 64 bytes, base64url, no padding
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: try #require(Data(base64URL: signatureText)))
        let message = DeviceRequestSigner.canonical(method: "POST", pathAndQuery: "/api/v1/rewards/r1/redeem", timestamp: timestamp,
                                                    nonce: "nonce-nonce-nonce-1", body: request.httpBody!, deviceId: Self.deviceId)
        let publicKey = try P256.Signing.PublicKey(x963Representation: key.publicKeyX963)
        #expect(publicKey.isValidSignature(signature, for: Data(message.utf8)))
    }

    @Test func noncesAreFreshAndWellFormed() {
        let a = DeviceRequestSigner.newNonce(), b = DeviceRequestSigner.newNonce()
        #expect(a != b)
        #expect((16...64).contains(a.count))
        #expect(a.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") })
    }

    @Test func clockOffsetCorrectsTimestamps() {
        let clock = DeviceClock(defaultsSuite: "org.opencozy.tests.\(UUID().uuidString)")
        let local = Date(timeIntervalSince1970: 1_800_000_000)
        clock.observe(serverTime: local.addingTimeInterval(300), receivedAt: local)
        #expect(clock.now(local: local) == local.addingTimeInterval(300))
    }

    @Test func parsesHTTPDateHeader() {
        #expect(DeviceClock.parseHTTPDate("Sat, 07 Nov 2026 10:00:00 GMT") == Date(timeIntervalSince1970: 1_794_045_600))
        #expect(DeviceClock.parseHTTPDate("yesterday") == nil)
    }
}

struct PairingInputTests {
    let token = String(repeating: "A", count: 40) + "b-_"

    @Test func parsesTheQRPayload() {
        let payload = "opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org&token=\(token)"
        #expect(PairingInput.parseQR(payload) == .token(origin: URL(string: "https://opencozy.example.org")!, token: token))
    }

    @Test func keepsAPort() {
        let payload = "opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org%3A8443&token=\(token)"
        #expect(PairingInput.parseQR(payload)?.origin == URL(string: "https://opencozy.example.org:8443")!)
    }

    @Test func refusesACleartextLANOrigin() {
        let payload = "opencozy-pair:v1?origin=http%3A%2F%2Fopencozy.local%3A3000&token=\(token)"
        #expect(PairingInput.parseQR(payload) == nil)
        #expect(PairingInput.parseCode("ABCD-EFGH", origin: "http://192.0.2.10:3000") == nil)
    }

    @Test(arguments: [
        "https://opencozy.example.org",                                                  // not a pairing code
        "opencozy-pair:v2?origin=https%3A%2F%2Fopencozy.example.org&token=TOKEN",        // unknown version
        "opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org",                    // no token
        "opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org&token=short",        // bad token
        "opencozy-pair:v1?origin=ftp%3A%2F%2Fopencozy.example.org&token=TOKEN",          // bad scheme
        "opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org%2Fevil&token=TOKEN", // origin with a path
    ])
    func rejectsOtherPayloads(_ payload: String) {
        #expect(PairingInput.parseQR(payload.replacingOccurrences(of: "TOKEN", with: token)) == nil)
    }

    @Test func normalisesTheTypedCode() {
        let origin = URL(string: "https://opencozy.example.org")!
        #expect(PairingInput.parseCode("abcd-efgh", origin: "https://opencozy.example.org") == .code(origin: origin, code: "ABCD-EFGH"))
        #expect(PairingInput.parseCode(" abcdefgh ", origin: "https://opencozy.example.org/") == .code(origin: origin, code: "ABCD-EFGH"))
    }

    @Test func rejectsBadCodes() {
        #expect(PairingInput.parseCode("ABCD-EFG", origin: "https://opencozy.example.org") == nil)  // too short
        #expect(PairingInput.parseCode("ABCD-EFG0", origin: "https://opencozy.example.org") == nil) // 0 isn't in the alphabet
        #expect(PairingInput.parseCode("ABCD-EFGH", origin: "not a url") == nil)
    }
}

@MainActor
struct PairingFlowTests {
    actor Recorder {
        var requests: [(URL, DevicePairRequest)] = []
        var failure: APIError?
        func record(_ origin: URL, _ r: DevicePairRequest) throws {
            requests.append((origin, r))
            if let failure { throw failure }
        }
        func fail(_ e: APIError) { failure = e }
    }

    struct FakePairing: DevicePairingAPI {
        let recorder: Recorder
        func pair(origin: URL, _ request: DevicePairRequest) async throws -> (DeviceInfo, receivedAt: Date) {
            try await recorder.record(origin, request)
            var info = try await FixtureAPI().device()
            info.device.id = "0f3c2a1e-5b6d-4e7f-8a9b-0c1d2e3f4a5b"
            info.serverTime = Date().addingTimeInterval(120)
            return (info, Date())
        }
    }

    private func isolatedConfig() -> AppConfig {
        let unique = "org.opencozy.tests.\(UUID().uuidString)"
        return AppConfig(keys: DeviceKeyStore(keychain: Keychain(service: unique)), clock: DeviceClock(defaultsSuite: unique), defaultsSuite: unique)
    }

    @Test func pairingSendsPublicKeyAndStoresTheDevice() async throws {
        let config = isolatedConfig()
        let model = AppModel(preview: FixtureAPI(), config: config)
        let recorder = Recorder()
        let origin = URL(string: "https://opencozy.example.org")!
        let token = String(repeating: "x", count: 43)
        let info = try await model.pair(.token(origin: origin, token: token), deviceName: "Robin's tablet", pairing: FakePairing(recorder: recorder))

        let (sentOrigin, request) = try #require(await recorder.requests.first)
        #expect(sentOrigin == origin)
        #expect(request.token == token && request.code == nil)
        #expect(request.platform == "ios")
        #expect(request.name == "Robin's tablet")
        let point = try #require(Data(base64Encoded: request.publicKey))
        #expect(point.count == 65 && point.first == 0x04)
        #expect(point.base64EncodedString() == request.publicKey) // standard, padded

        // Proof of possession: raw r||s, base64url without padding, over the exact message.
        #expect(request.proof.count == 86)
        #expect(!request.proof.contains("="))
        let publicKey = try P256.Signing.PublicKey(x963Representation: point)
        let proof = try P256.Signing.ECDSASignature(rawRepresentation: try #require(Data(base64URL: request.proof)))
        let message = "OPENCOZY-PAIR-V1\n\(token)\n\(request.publicKey)"
        #expect(publicKey.isValidSignature(proof, for: Data(message.utf8)))
        #expect(!publicKey.isValidSignature(proof, for: Data((message + "\n").utf8)))

        #expect(config.deviceId == info.device.id)
        #expect(config.baseURL == origin)
        #expect(config.keys.load()?.publicKeyX963 == point)
        #expect(abs(config.clock.offset - 120) < 5)
        #expect(config.isConfigured)
    }

    @Test func failedPairingKeepsThePreviousPairing() async throws {
        let config = isolatedConfig()
        let model = AppModel(preview: FixtureAPI(), config: config)
        let recorder = Recorder()
        let origin = URL(string: "https://opencozy.example.org")!
        _ = try await model.pair(.code(origin: origin, code: "ABCD-EFGH"), deviceName: "Tablet", pairing: FakePairing(recorder: recorder))
        let before = config.keys.load()?.publicKeyX963
        await recorder.fail(.server(status: 409, message: "Pairing unavailable."))
        await #expect(throws: APIError.self) {
            try await model.pair(.code(origin: origin, code: "WXYZ-2345"), deviceName: "Tablet", pairing: FakePairing(recorder: recorder))
        }
        #expect(config.keys.load()?.publicKeyX963 == before)
        #expect(config.deviceId == "0f3c2a1e-5b6d-4e7f-8a9b-0c1d2e3f4a5b")
    }

    @Test func codeProofSignsTheCodeAsSent() async throws {
        let config = isolatedConfig()
        let model = AppModel(preview: FixtureAPI(), config: config)
        let recorder = Recorder()
        _ = try await model.pair(.code(origin: URL(string: "https://opencozy.example.org")!, code: "ABCD-EFGH"), deviceName: "Tablet",
                                 pairing: FakePairing(recorder: recorder))
        let (_, request) = try #require(await recorder.requests.first)
        #expect(request.code == "ABCD-EFGH" && request.token == nil)
        let publicKey = try P256.Signing.PublicKey(x963Representation: try #require(Data(base64Encoded: request.publicKey)))
        let proof = try P256.Signing.ECDSASignature(rawRepresentation: try #require(Data(base64URL: request.proof)))
        #expect(publicKey.isValidSignature(proof, for: Data("OPENCOZY-PAIR-V1\nABCD-EFGH\n\(request.publicKey)".utf8)))
        // The stored key is the one that made the proof.
        #expect(config.keys.load()?.publicKeyX963 == publicKey.x963Representation)
    }

    @Test func codeRequestOmitsToken() throws {
        let request = DevicePairRequest(token: nil, code: "ABCD-EFGH", publicKey: "BA==", platform: "ios", name: "x")
        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
        #expect(json?.keys.sorted() == ["code", "name", "platform", "proof", "publicKey"])
    }

    @Test func unpairForgetsKeyAndDevice() async throws {
        let config = isolatedConfig()
        let model = AppModel(preview: FixtureAPI(), config: config)
        _ = try await model.pair(.code(origin: URL(string: "https://opencozy.example.org")!, code: "ABCD-EFGH"), deviceName: "Tablet",
                                 pairing: FakePairing(recorder: Recorder()))
        model.unpair()
        #expect(config.deviceId == nil)
        #expect(config.keys.load() == nil)
        #expect(!config.isConfigured)
    }
}

@MainActor
struct UnassignedDeviceTests {
    struct UnassignedAPI: OpenCozyAPI {
        func device() async throws -> DeviceInfo {
            var info = try await FixtureAPI().device()
            info.device.child = nil
            return info
        }
        func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) { throw APIError.transport("unused") }
    }

    @Test func deviceWithoutAChildIsLocked() async {
        let model = AppModel(preview: UnassignedAPI())
        await model.refresh()
        #expect(model.accessProblem == AppModel.reconnectMessage)
        #expect(model.unlockedUntil == nil)
        #expect(model.childId == nil)
    }
}

struct PasskeyWireTests {
    @Test func authenticationResponseJSONShape() throws {
        let body = AuthenticationResponseJSON(PasskeyAssertion(credentialId: "Y3JlZA", clientDataJSON: "Y2Q", authenticatorData: "YWQ",
                                                               signature: "c2ln", userHandle: "dQ"))
        let json = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any])
        #expect(json["id"] as? String == "Y3JlZA")
        #expect(json["rawId"] as? String == "Y3JlZA")
        #expect(json["type"] as? String == "public-key")
        #expect((json["clientExtensionResults"] as? [String: Any])?.isEmpty == true)
        let response = try #require(json["response"] as? [String: Any])
        #expect(response.keys.sorted() == ["authenticatorData", "clientDataJSON", "signature", "userHandle"])
    }
}

@MainActor
struct PasskeySignInTests {
    @Test func passkeySignInUsesServerRelyingPartyAndChallenge() async throws {
        let server = FakeParentServer(approvals: [])
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: .none)
        var seen: (String, Data)?
        let ok = await model.signInWithPasskey(server: "https://opencozy.example.org", deviceName: "Test phone") { rpId, challenge in
            seen = (rpId, challenge)
            return PasskeyAssertion(credentialId: "Y3JlZA", clientDataJSON: "Y2Q", authenticatorData: "YWQ", signature: "c2ln", userHandle: nil)
        }
        #expect(ok)
        #expect(seen?.0 == "opencozy.example.org")
        #expect(seen?.1 == Data("challenge-bytes".utf8))
        #expect(store.token == "ocp_passkey")
        let sent = try #require(await server.passkeyRequests.first)
        #expect(sent.challengeId == "ch1")
        #expect(sent.deviceName == "Test phone")
    }

    @Test func cancelledPasskeyIsQuiet() async {
        let server = FakeParentServer(approvals: [])
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: .none)
        let ok = await model.signInWithPasskey(server: "https://opencozy.example.org", deviceName: "x") { _, _ in
            throw PasskeyAuthenticator.PasskeyError.cancelled
        }
        #expect(!ok)
        #expect(model.errorMessage == nil)
        #expect(store.token == nil)
    }
}
