import Foundation
import Testing
@testable import OpenCozy

/// Opt-in, against a local development server only (never a real installation):
/// the app's own pairing, signing and device client end to end. Set
/// `TEST_RUNNER_OPENCOZY_E2E_PAIRING` to a fresh pairing's QR payload
/// (`opencozy-pair:v1?origin=…&token=…`, from `POST /api/devices/pairings`) and run
/// `scripts/ci.sh`. Skipped otherwise, and always in CI.
@Suite(.serialized, .enabled(if: LocalServer.pairing != nil))
struct LocalServerTests {
    @Test func pairsSignsAndOpensTheKidView() async throws {
        let input = try #require(LocalServer.pairing)
        guard case .token(let origin, let token) = input else { return }
        let host = try #require(origin.host())
        #expect(host == "127.0.0.1" || host == "localhost", "Only a local server.")
        let session = URLSession(configuration: .ephemeral)

        // Pair as AppModel.pair does: a new key, the proof, platform and name.
        let keys = DeviceKeyStore(keychain: Keychain(service: "org.opencozy.tests.\(UUID().uuidString)"))
        let (key, _) = try keys.generate()
        var request = DevicePairRequest(token: token, publicKey: key.publicKeyX963.base64EncodedString(),
                                        platform: DevicePlatform.current, name: "Robin's iPad")
        try request.prove(with: key)
        let (paired, _) = try await HTTPDevicePairingAPI(session: session).pair(origin: origin, request)
        #expect(paired.device.platform == "ios")
        #expect(paired.device.name == "Robin's iPad")
        #expect(paired.device.childId != nil)
        #expect(!paired.household.timeZone.isEmpty)

        // The same token again: refused, with the new error body.
        do {
            _ = try await HTTPDevicePairingAPI(session: session).pair(origin: origin, request)
            Issue.record("A used pairing token was accepted again.")
        } catch APIError.server(let status, _) {
            #expect(status == 409)
        }

        // Signed requests.
        let signer = DeviceRequestSigner(deviceId: paired.device.id, key: key)
        let api = HTTPOpenCozyAPI(baseURL: origin, signer: signer, session: session)
        let info = try await api.device()
        #expect(info.device.id == paired.device.id)
        #expect(info.device.child == paired.device.child)
        #expect(info.household.lockMessage == paired.household.lockMessage)

        // A request signed by another key is refused.
        let stranger = HTTPOpenCozyAPI(baseURL: origin, signer: DeviceRequestSigner(deviceId: paired.device.id, key: try keys.generate().key), session: session)
        do {
            _ = try await stranger.device()
            Issue.record("A request signed by the wrong key was accepted.")
        } catch APIError.server(let status, _) {
            #expect(status == 401)
        }

        // The child's iPad time, signed for the device's own child.
        let childId = try #require(info.device.childId)
        let (screen, _) = try await api.screenTime(childId: childId)
        #expect(screen.childId == childId)
        #expect(screen.allowanceSeconds <= 300)

        // The kid view: a one-time exchange URL on this origin that sets the device cookie.
        let link = try await api.webSession()
        let exchange = try #require(DeviceWebSession.resolve(link.url, origin: origin))
        let (_, response) = try await URLSession(configuration: .ephemeral, delegate: NoRedirects(), delegateQueue: nil).data(from: exchange)
        let http = try #require(response as? HTTPURLResponse)
        #expect(http.statusCode == 303)
        #expect(http.value(forHTTPHeaderField: "Location") == "/kid")
        let cookie = try #require(http.value(forHTTPHeaderField: "Set-Cookie"))
        #expect(cookie.hasPrefix("__Host-device="))

        // The cookie is this child's kid session.
        var me = URLRequest(url: origin.appending(path: "api/device/session"))
        me.setValue(String(cookie.prefix { $0 != ";" }), forHTTPHeaderField: "Cookie")
        let (body, meResponse) = try await session.data(for: me)
        #expect((meResponse as? HTTPURLResponse)?.statusCode == 200)
        let kid = try JSONSerialization.jsonObject(with: body) as? [String: Any]
        #expect((kid?["child"] as? [String: Any])?["id"] as? String == paired.device.childId)

        // The link is single use.
        let (_, again) = try await URLSession(configuration: .ephemeral, delegate: NoRedirects(), delegateQueue: nil).data(from: exchange)
        #expect((again as? HTTPURLResponse)?.value(forHTTPHeaderField: "Location")?.hasPrefix("/device/expired") == true)
    }
}

private enum LocalServer {
    static let pairing = ProcessInfo.processInfo.environment["OPENCOZY_E2E_PAIRING"].flatMap(PairingInput.parseQR)
}

private final class NoRedirects: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest) async -> URLRequest? { nil }
}
