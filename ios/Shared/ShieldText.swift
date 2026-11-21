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
    /// The end of the running window when the shield came back only at the allowance's cap
    /// (the app wasn't open to sync): the child still has time until then.
    public var runsUntil: Date?

    public init(message: String, bankedSeconds: Int = 0, opensAt: Date? = nil, closesAt: Date? = nil, ask: ShieldAsk? = nil,
                runsUntil: Date? = nil) {
        self.runsUntil = runsUntil
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
    /// A parent said no (shown for an hour). The server refuses a new ask for 5 minutes.
    case declined
}

/// Composes the shield subtitle. Pure, so it is unit tested. The words match the kid
/// view (/kid) in the web app's design system: sentence case, "min", times like 7:30 PM.
public enum ShieldText {
    /// Shown before the first sync, and whenever the stored message is blank.
    /// Matches the server's default lock message (GET /api/device `household.lockMessage`).
    public static let defaultMessage = "Earn time on the chore board, then spend your points in OpenCozy."

    /// "Paused" while the window still runs (`runsUntil`), otherwise "Locked".
    public static func title(_ content: ShieldContent?, now: Date, name: String?) -> String {
        let paused = content?.runsUntil.map { $0 > now } ?? false
        return name.map { "\($0) is \(paused ? "paused" : "locked")" } ?? (paused ? "Paused" : "Locked")
    }

    /// While the window still runs (`runsUntil`), only that. Otherwise the lock message, then when screen time starts (while allowed hours are
    /// closed), then either where a request for more time stands or, with none, the
    /// banked time. `queued` is a request made on this shield that the app has not
    /// sent yet; `canOpenApp` whether the shield's button opens it.
    public static func subtitle(_ content: ShieldContent?, now: Date, queued: Bool = false, canOpenApp: Bool = true,
                                timeZone: TimeZone = .current, locale: Locale = Locale(identifier: "en_US")) -> String {
        // Time is still running on the server: say so, not "time is over".
        if let runsUntil = content?.runsUntil, runsUntil > now {
            return "Your time runs until \(clock(runsUntil, timeZone: timeZone, locale: locale)). Open OpenCozy to carry on."
        }
        let message = content?.message.trimmingCharacters(in: .whitespacesAndNewlines)
        var lines = [message.flatMap { $0.isEmpty ? nil : $0 } ?? defaultMessage]
        if let content {
            // A closed window is only "closed" until it opens; a stale sync whose
            // opening has passed says nothing about hours.
            let closed = content.opensAt.map { $0 > now } ?? false
            if let opensAt = content.opensAt, closed {
                lines.append("Screen time starts at \(clock(opensAt, timeZone: timeZone, locale: locale))\(dayWord(opensAt, now: now, timeZone: timeZone)).")
            }
            let minutes = content.bankedSeconds / 60
            if minutes > 0, askLine(content.ask, queued: queued, canOpenApp: canOpenApp) == nil {
                let saved = "You have \(minutes) min saved."
                lines.append(closed ? saved : "\(saved) Open OpenCozy to use it.")
            }
        }
        // The ask status takes the saved line's place while it applies.
        if let line = askLine(content?.ask, queued: queued, canOpenApp: canOpenApp) { lines.append(line) }
        return lines.joined(separator: "\n\n")
    }

    /// A request made here and not sent yet comes first: without the 26.5
    /// button the child has to open OpenCozy to send it.
    static func askLine(_ ask: ShieldAsk?, queued: Bool, canOpenApp: Bool) -> String? {
        if queued { return canOpenApp ? pendingLine : "Open OpenCozy to send it." }
        switch ask {
        case .pending: return pendingLine
        case .declined: return "A parent said not right now. You can ask again in a few minutes."
        case nil: return nil
        }
    }

    static let pendingLine = "You asked a parent for more time. Waiting for an answer."

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
