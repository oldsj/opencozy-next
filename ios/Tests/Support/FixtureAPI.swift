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

    func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) {
        (Self.screenTime(unlockedMinutes: unlockedMinutes), Date())
    }

    /// The child's iPad time now: a window ending in `unlockedMinutes` (the allowance as the
    /// server caps it), or locked.
    static func screenTime(unlockedMinutes: Int?, now: Date = Date(), allowance: Int = 300) -> ScreenTime {
        let windows = unlockedMinutes.map { minutes in
            [ScreenTimeWindow(id: "00000000-0000-4000-8000-0000000000d1",
                              startsAt: now.addingTimeInterval(Double(minutes * 60 - 15 * 60)),
                              endsAt: now.addingTimeInterval(Double(minutes * 60)))]
        } ?? []
        let until = windows.last?.endsAt
        return ScreenTime(serverTime: now, childId: childId, bankedSeconds: 0, unlockedUntil: until,
                          allowanceSeconds: until.map { min(Int($0.timeIntervalSince(now)), allowance) } ?? 0,
                          windows: windows)
    }

    func webSession() async throws -> DeviceWebSession {
        DeviceWebSession(url: "/device/session?token=example-one-time-token", expiresAt: Date().addingTimeInterval(60))
    }
}
