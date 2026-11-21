import Foundation
import Testing
@testable import OpenCozy

/// The kid app against this server's devices contract (api/src/devices.yaml, slice 7).
struct DevicesWireTests {
    static let deviceSelf = #"""
    { "device": { "id": "0195f2d4-0000-7000-8000-0000000000d1", "name": "Robin's iPad", "platform": "ios",
                  "child": { "id": "0195f2d4-0000-7000-8000-0000000000c1", "name": "Robin", "color": "sky" },
                  "pairedAt": "2026-11-15T10:00:00Z", "lastSeenAt": "2026-11-15T10:05:00.123Z" },
      "household": { "id": "0195f2d4-0000-7000-8000-0000000000a1", "name": "The Example House", "timeZone": "America/Chicago",
                     "lockMessage": "Earn time on the chore board, then spend your points in OpenCozy." },
      "serverTime": "2026-11-15T10:05:01Z" }
    """#

    @Test func deviceSelfDecodes() throws {
        let info = try APITransport.decode(DeviceInfo.self, from: Data(Self.deviceSelf.utf8))
        #expect(info.device.platform == "ios")
        #expect(info.household.timeZone == "America/Chicago")
        #expect(info.device.childId == "0195f2d4-0000-7000-8000-0000000000c1")
        #expect(info.device.childName == "Robin")
        #expect(info.household.lockMessage.hasPrefix("Earn time"))
        #expect(info.device.lastSeenAt != nil)
    }

    /// An archived child: `child` is null and `lastSeenAt` may be absent.
    @Test func deviceWithoutChildDecodes() throws {
        let json = Self.deviceSelf
            .replacingOccurrences(of: #"{ "id": "0195f2d4-0000-7000-8000-0000000000c1", "name": "Robin", "color": "sky" }"#, with: "null")
            .replacingOccurrences(of: #", "lastSeenAt": "2026-11-15T10:05:00.123Z""#, with: "")
        let info = try APITransport.decode(DeviceInfo.self, from: Data(json.utf8))
        #expect(info.device.child == nil && info.device.childId == nil)
        #expect(info.device.lastSeenAt == nil)
    }

    @Test func pairRequestUsesContractFields() throws {
        let request = DevicePairRequest(token: "t", code: nil, publicKey: "BA==", platform: "ios", name: "Robin's iPhone")
        let json = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any])
        #expect(json.keys.sorted() == ["name", "platform", "proof", "publicKey", "token"])
        #expect(json["name"] as? String == "Robin's iPhone")
    }

    @Test func platformIsIOS() {
        #expect(DevicePlatform.current == "ios")
    }

    @Test func errorMessageFromEitherBody() {
        let current = Data(#"{"error":{"code":"pairing_unavailable","message":"That code doesn't work any more."}}"#.utf8)
        #expect(APITransport.ErrorBody.message(in: current) == "That code doesn't work any more.")
        #expect(APITransport.ErrorBody.message(in: Data(#"{"error":"Pairing unavailable."}"#.utf8)) == "Pairing unavailable.")
        #expect(APITransport.ErrorBody.message(in: Data("<html>".utf8)) == nil)
    }

}

/// A failure to read screen time fails the sync, and the device keeps only the window it
/// already had (locked here).
@MainActor
struct ScreenTimeSyncTests {
    @Test func failuresFailTheSync() async {
        struct Down: OpenCozyAPI {
            func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
            func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) {
                throw APIError.server(status: 503, message: "Unavailable")
            }
        }
        let model = AppModel(preview: Down())
        await model.refresh()
        #expect(model.syncFailed)
        #expect(model.unlockedUntil == nil)
    }

    @Test func anotherChildsTimeNeverUnlocks() async {
        struct Other: OpenCozyAPI {
            func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
            func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) {
                var screen = FixtureAPI.screenTime(unlockedMinutes: 20)
                screen.childId = "00000000-0000-4000-8000-0000000000c9"
                return (screen, Date())
            }
        }
        let model = AppModel(preview: Other())
        await model.refresh()
        #expect(model.unlockedUntil == nil)
    }
}
