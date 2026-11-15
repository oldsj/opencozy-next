import Foundation
import Testing
@testable import OpenCozy

/// The kid app against this server's devices contract (api/src/devices.yaml, slice 7).
struct DevicesWireTests {
    static let deviceSelf = #"""
    { "device": { "id": "0195f2d4-0000-7000-8000-0000000000d1", "name": "Robin's iPad", "platform": "ipad",
                  "child": { "id": "0195f2d4-0000-7000-8000-0000000000c1", "name": "Robin", "color": "sky" },
                  "pairedAt": "2026-11-15T10:00:00Z", "lastSeenAt": "2026-11-15T10:05:00.123Z" },
      "household": { "id": "0195f2d4-0000-7000-8000-0000000000a1", "name": "The Example House",
                     "lockMessage": "Earn time on the chore board, then spend your points in OpenCozy." },
      "serverTime": "2026-11-15T10:05:01Z" }
    """#

    @Test func deviceSelfDecodes() throws {
        let info = try APITransport.decode(DeviceInfo.self, from: Data(Self.deviceSelf.utf8))
        #expect(info.device.platform == "ipad")
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
        let request = DevicePairRequest(token: "t", code: nil, publicKey: "BA==", platform: "iphone", name: "Robin's iPhone")
        let json = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any])
        #expect(json.keys.sorted() == ["name", "platform", "proof", "publicKey", "token"])
        #expect(json["name"] as? String == "Robin's iPhone")
    }

    @Test func platformFollowsTheIdiom() {
        #expect(DevicePlatform.current(idiom: .pad) == "ipad")
        #expect(DevicePlatform.current(idiom: .phone) == "iphone")
    }

    @Test func errorMessageFromEitherBody() {
        let current = Data(#"{"error":{"code":"pairing_unavailable","message":"That code doesn't work any more."}}"#.utf8)
        #expect(APITransport.ErrorBody.message(in: current) == "That code doesn't work any more.")
        #expect(APITransport.ErrorBody.message(in: Data(#"{"error":"Pairing unavailable."}"#.utf8)) == "Pairing unavailable.")
        #expect(APITransport.ErrorBody.message(in: Data("<html>".utf8)) == nil)
    }

    @Test func missingEndpointIsOnly404Or405() {
        #expect(APIError.server(status: 404, message: "").isMissingEndpoint)
        #expect(APIError.server(status: 405, message: "").isMissingEndpoint)
        #expect(!APIError.server(status: 401, message: "").isMissingEndpoint)
        #expect(!APIError.transport("offline").isMissingEndpoint)
    }
}

/// Until slice 8 lands, this server answers the screen-time calls 404. The device
/// must stay locked, keep what it would send queued, and not report a failed sync.
@MainActor
struct NotYetServedTests {
    actor Calls {
        var events = 0
        func event() { events += 1 }
    }

    struct SliceSevenServer: OpenCozyAPI {
        let calls: Calls
        func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
            throw APIError.server(status: 404, message: "Not found")
        }
        func screenTimeEvents(_ events: [ScreenTimeEventUpload]) async throws -> Set<String> {
            await calls.event()
            throw APIError.server(status: 404, message: "Not found")
        }
    }

    @Test func missingScreenTimeLocksWithoutAnError() async {
        let model = AppModel(preview: SliceSevenServer(calls: Calls()))
        await model.refresh()
        #expect(model.unlockedUntil == nil)
        #expect(!model.syncFailed)
        #expect(model.errorMessage == nil)
        #expect(model.accessProblem == nil)
        #expect(model.childName == "Robin")
    }

    @Test func otherFailuresStillFailTheSync() async {
        struct Down: OpenCozyAPI {
            func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
            func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
                throw APIError.server(status: 503, message: "Unavailable")
            }
        }
        let model = AppModel(preview: Down())
        await model.refresh()
        #expect(model.syncFailed)
    }

    @Test func untilServedSkipsOnlyMissingEndpoints() async {
        await #expect(throws: Never.self) { try await AppModel.untilServed { throw APIError.server(status: 404, message: "") } }
        await #expect(throws: APIError.self) { try await AppModel.untilServed { throw APIError.server(status: 401, message: "") } }
    }
}
