import Foundation
@testable import OpenCozy

/// Fictional data for tests.
struct FixtureAPI: OpenCozyAPI {
    static let childId = "00000000-0000-4000-8000-0000000000c1"
    var unlockedMinutes: Int?

    func device() async throws -> DeviceInfo {
        DeviceInfo(device: DeviceRecord(id: "00000000-0000-4000-8000-0000000000a1", name: "Robin's tablet", platform: "ios",
                                        child: DeviceChild(id: Self.childId, name: "Robin", color: "sky"), pairedAt: Date(), lastSeenAt: Date()),
                   household: DeviceHousehold(id: "00000000-0000-4000-8000-0000000000a2", name: "The Example House", timeZone: "America/Chicago",
                                              lockMessage: "Earn time on the chore board, then spend your points in OpenCozy."),
                   serverTime: Date())
    }

    func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
        let now = Date()
        let grants = unlockedMinutes.map { minutes in
            [ScreenTimeGrant(redemptionId: "00000000-0000-4000-8000-0000000000d1", minutes: 15,
                             startsAt: now.addingTimeInterval(Double(minutes * 60 - 15 * 60)),
                             endsAt: now.addingTimeInterval(Double(minutes * 60)))]
        } ?? []
        let entry = ChildScreenTime(childId: Self.childId, target: target.rawValue, activeGrants: grants, unlockedUntil: grants.last?.endsAt)
        return (ScreenTimeResponse(serverTime: now, children: [entry]), now)
    }

    func webSession() async throws -> DeviceWebSession {
        DeviceWebSession(url: "/device/session?token=example-one-time-token", expiresAt: Date().addingTimeInterval(60))
    }
}
