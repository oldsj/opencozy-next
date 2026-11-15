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

enum ScreenTimeTarget: String, Codable, Sendable {
    case personal
    case tv
}

struct ScreenTimeGrant: Decodable, Equatable, Sendable {
    var redemptionId: String
    var minutes: Int
    var startsAt: Date
    var endsAt: Date
}

struct ChildScreenTime: Decodable, Equatable, Sendable {
    var childId: String
    var target: String
    var activeGrants: [ScreenTimeGrant]
    var unlockedUntil: Date?
    /// Banked iPad time not yet spent.
    var secondsRemaining: Int?
    var allowedHours: AllowedHours?
    /// The child's latest "Ask for more time", while pending or for an hour after the answer.
    var timeRequest: TimeRequestStatus?
}

/// A request for more time and, once a parent answers, the outcome.
struct TimeRequestStatus: Decodable, Equatable, Sendable {
    enum State: String, Decodable, Sendable { case pending, approved, declined }
    var id: String
    var state: State
    var grantedUntil: Date?
}

/// `{ open, closesAt, opensAt }`, or null when the child has no rule.
struct AllowedHours: Decodable, Equatable, Sendable {
    var open: Bool
    var closesAt: Date?
    var opensAt: Date?
}

/// `GET /api/v1/screen-time?target=personal` (slice 8: not on the new server yet) → `{ serverTime, children: [...] }`.
struct ScreenTimeResponse: Decodable, Equatable, Sendable {
    var serverTime: Date
    var children: [ChildScreenTime]
    /// The household's blocked websites (personal target only).
    var webFilter: WebFilterLists?
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
