import Foundation

// Wire models for the OpenCozy API (api/openapi.yaml). Decoding is deliberately
// tolerant of extra fields so the server can add fields without breaking the app.

/// The device's child: `{ id, name, color }`.
struct DeviceChild: Decodable, Equatable, Sendable {
    var id: String
    var name: String
    var color: String?
}

/// A paired child's device (api/src/devices.yaml `Device`). The device is its own
/// object; the server says which child it belongs to, and an adult can change that.
/// `child` is null while the device's child is archived.
struct DeviceRecord: Decodable, Equatable, Sendable {
    var id: String
    var name: String
    /// `ipad` or `iphone`.
    var platform: String
    var child: DeviceChild?
    var pairedAt: Date
    var lastSeenAt: Date?

    var childId: String? { child?.id }
    var childName: String? { child?.name }
}

/// `{ id, name, lockMessage }`. `lockMessage` is always a string: the household's
/// own words, or the server's default.
struct DeviceHousehold: Decodable, Equatable, Sendable {
    var id: String
    var name: String
    /// IANA zone name; the household day follows it.
    var timeZone: String
    var lockMessage: String
}

/// `GET /api/device` and the pairing response (`DeviceSelf`).
struct DeviceInfo: Decodable, Equatable, Sendable {
    var device: DeviceRecord
    var household: DeviceHousehold
    var serverTime: Date
}

/// The pairing request's `platform` (`ios` or `android`). An iPad and an iPhone
/// are both `ios`; the name tells them apart.
enum DevicePlatform {
    static let current = "ios"
}

/// A running or queued unlock window (`Window`).
struct ScreenTimeWindow: Decodable, Equatable, Sendable {
    var id: String
    var startsAt: Date
    /// Its effective end, if a parent ended it early.
    var endsAt: Date
}

/// A request for more time and, once a parent answers, the outcome (`TimeRequest`).
struct TimeRequestStatus: Decodable, Equatable, Sendable {
    enum State: String, Decodable, Sendable { case pending, approved, declined }
    var id: String
    var state: State
    var grantedUntil: Date?
}

/// Where the child stands against their allowed hours (`HoursNow`): open until
/// `closesAt`, or closed until `opensAt`. Absent when the child has no rule.
struct HoursNow: Decodable, Equatable, Sendable {
    var open: Bool
    var closesAt: Date?
    var opensAt: Date?
}

/// `GET /api/children/{childId}/screen-time` (`ScreenTime`), signed by the device for
/// its own child.
struct ScreenTime: Decodable, Equatable, Sendable {
    var serverTime: Date
    var childId: String
    /// Banked, not yet spent.
    var bankedSeconds: Int
    /// The end of the run of windows covering now; absent while locked.
    var unlockedUntil: Date?
    /// How long past `serverTime` the device may stay unlocked without asking again (≤ 300 s).
    var allowanceSeconds: Int
    var windows: [ScreenTimeWindow]
    var hoursNow: HoursNow?
    /// The latest "Ask for more time", while it waits and for an hour after the answer.
    var timeRequest: TimeRequestStatus?
    /// The household's blocked websites (slice 8b); absent from a server without them.
    var webFilter: WebFilterLists?

    /// Whether the device locks within the allowance (design §3 rule 6: a device that can't
    /// reach the server locks within 300 s) rather than at the end of the window. While the app
    /// is open it syncs before then and stays unlocked; in the background nothing can sync, so
    /// the child opens OpenCozy to carry on. The one switch for that trade-off.
    static let locksWithinAllowance = true

    /// When the device locks, in server time: the end of the running window, capped at the
    /// allowance past `serverTime` (`locksWithinAllowance`). Nil while locked.
    var lockAt: Date? {
        guard let unlockedUntil else { return nil }
        guard Self.locksWithinAllowance else { return unlockedUntil }
        return min(unlockedUntil, serverTime.addingTimeInterval(TimeInterval(max(0, allowanceSeconds))))
    }

    /// Whether `lockAt` comes before the window's end: more time follows a sync.
    var isCapped: Bool {
        guard let lockAt, let unlockedUntil else { return false }
        return lockAt < unlockedUntil
    }
}

/// `POST /api/device/web-session` → a one-time exchange URL (relative to the
/// origin, or absolute on it) that sets the child-device web session cookie.
struct DeviceWebSession: Decodable, Equatable, Sendable {
    var url: String
    var expiresAt: Date?

    /// The exchange URL, only if it's `/device/session` on the paired origin (same
    /// scheme, host and port; the pairing already required https or this machine).
    /// Anything else is refused rather than opened.
    static func resolve(_ text: String, origin: URL) -> URL? {
        guard let url = URL(string: text, relativeTo: origin)?.absoluteURL, WebPolicy.isSameOrigin(url, origin),
              url.path(percentEncoded: false) == "/device/session" else { return nil }
        return url
    }
}

enum APIDates {
    /// `Date.toISOString()` output, with or without fractional seconds.
    static func decode(_ decoder: Decoder) throws -> Date {
        let string = try decoder.singleValueContainer().decode(String.self)
        if let date = try? Date(string, strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true).timeZone(separator: .omitted)) {
            return date
        }
        if let date = try? Date(string, strategy: .iso8601) {
            return date
        }
        throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Invalid ISO 8601 date: \(string)"))
    }

    static func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom(decode)
        return d
    }
}
