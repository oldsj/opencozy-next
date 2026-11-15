import Foundation
@testable import OpenCozy

/// Fictional data for tests.
struct FixtureAPI: OpenCozyAPI {
    static let childId = "00000000-0000-4000-8000-0000000000c1"
    var unlockedMinutes: Int?

    func device() async throws -> DeviceInfo {
        DeviceInfo(device: DeviceRecord(id: "00000000-0000-4000-8000-0000000000a1", name: "Robin's tablet", platform: "ios",
                                        childId: Self.childId, childName: "Robin", pairedAt: Date(), lastSeenAt: Date(), revokedAt: nil),
                   household: HouseholdInfo(id: "00000000-0000-4000-8000-0000000000a2", name: "The Example House"),
                   scopes: ["screentime:read"], serverTime: Date())
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
