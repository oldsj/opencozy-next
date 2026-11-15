import Foundation

/// What the shield says, synced by the app into the App Group so the
/// ShieldConfiguration extension (which has no network) can show it offline.
///
/// `message` is the household's lock message (an adult edits it in Manage →
/// Devices). `bankedSeconds`, `opensAt` and `closesAt` come from the last
/// screen-time sync, in device time (already corrected for server skew).
public struct ShieldContent: Codable, Equatable, Sendable {
    public var message: String
    public var bankedSeconds: Int
    public var opensAt: Date?
    public var closesAt: Date?
    /// The latest "Ask for more time" the server knows of.
    public var ask: ShieldAsk?

    public init(message: String, bankedSeconds: Int = 0, opensAt: Date? = nil, closesAt: Date? = nil, ask: ShieldAsk? = nil) {
        self.message = message
        self.bankedSeconds = bankedSeconds
        self.opensAt = opensAt
        self.closesAt = closesAt
        self.ask = ask
    }
}

/// Where the child's request for more time stands, as the shield shows it.
public enum ShieldAsk: String, Codable, Sendable {
    /// Waiting for a parent.
    case pending
    /// A parent said no (shown for an hour).
    case declined
}

/// Composes the shield subtitle. Pure, so it is unit tested.
public enum ShieldText {
    /// Shown before the first sync, and whenever the stored message is blank.
    /// Matches the server default (src/lib/server/household.ts).
    public static let defaultMessage = "Earn time on the chore board, then spend your points in OpenCozy."

    /// The lock message, then when screen time opens (while allowed hours are
    /// closed), then the banked time (while it can be used now), then a
    /// request for more time. `queued` is a request made on this shield that
    /// the app has not sent yet; `canOpenApp` whether the shield's button opens it.
    public static func subtitle(_ content: ShieldContent?, now: Date, queued: Bool = false, canOpenApp: Bool = true,
                                timeZone: TimeZone = .current, locale: Locale = Locale(identifier: "en_US")) -> String {
        let message = content?.message.trimmingCharacters(in: .whitespacesAndNewlines)
        var lines = [message.flatMap { $0.isEmpty ? nil : $0 } ?? defaultMessage]
        if let content {
            // A closed window is only "closed" until it opens; a stale sync whose
            // opening has passed says nothing about hours.
            let closed = content.opensAt.map { $0 > now } ?? false
            if let opensAt = content.opensAt, closed {
                lines.append("Screen time opens at \(clock(opensAt, timeZone: timeZone, locale: locale))\(dayWord(opensAt, now: now, timeZone: timeZone)).")
            }
            let minutes = content.bankedSeconds / 60
            if minutes > 0 {
                let saved = "You have \(minutes) min saved."
                lines.append(closed ? saved : "\(saved) Open OpenCozy to use it.")
            }
        }
        if let line = askLine(content?.ask, queued: queued, canOpenApp: canOpenApp) { lines.append(line) }
        return lines.joined(separator: "\n\n")
    }

    /// A request made here and not sent yet comes first: without the 26.5
    /// button the child has to open OpenCozy to send it.
    static func askLine(_ ask: ShieldAsk?, queued: Bool, canOpenApp: Bool) -> String? {
        if queued { return canOpenApp ? "You asked a parent for more time." : "Open OpenCozy to send your request for more time." }
        switch ask {
        case .pending: return "You asked a parent for more time. Waiting for an answer."
        case .declined: return "A parent said not right now."
        case nil: return nil
        }
    }

    /// The secondary button: asking again while one is pending is harmless.
    public static func askButton(_ ask: ShieldAsk?, queued: Bool) -> String {
        queued || ask == .pending ? "Asked for more time" : "Ask for more time"
    }

    static func clock(_ date: Date, timeZone: TimeZone, locale: Locale) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = timeZone
        formatter.dateFormat = "h:mm a"
        return formatter.string(from: date)
    }

    /// "" today, " tomorrow", or " on Monday" further out.
    static func dayWord(_ date: Date, now: Date, timeZone: TimeZone) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: now), to: calendar.startOfDay(for: date)).day ?? 0
        switch days {
        case ...0: return ""
        case 1: return " tomorrow"
        default:
            let formatter = DateFormatter()
            formatter.locale = Locale(identifier: "en_US")
            formatter.timeZone = timeZone
            formatter.dateFormat = "EEEE"
            return " on \(formatter.string(from: date))"
        }
    }
}
