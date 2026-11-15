import Foundation

/// How to express an unlock window as a DeviceActivity schedule.
///
/// Apple documents a minimum monitoring interval of fifteen minutes
/// (`DeviceActivityCenter.MonitoringError.intervalTooShort`). For a window shorter than
/// that, the schedule is stretched past `endsAt` and `warningTime` is set so that
/// `intervalWillEndWarning` fires at `endsAt`; `intervalDidEnd` stays as a backstop.
public struct MonitorPlan: Equatable, Sendable {
    public var start: Date
    public var end: Date
    /// Whole minutes before `end` at which `intervalWillEndWarning` should fire.
    public var warningMinutes: Int?

    /// The instant the shield should come back: `end` minus any warning.
    public var lockAt: Date { end.addingTimeInterval(-Double((warningMinutes ?? 0) * 60)) }

    /// Apple's documented minimum is 15 minutes. Plan one extra minute so that any
    /// second-level rounding inside the system can't push the interval below it.
    public static let minimumInterval: TimeInterval = 16 * 60
    /// Apple's forums report a one-week maximum (`intervalTooLong`); stay under it.
    public static let maximumInterval: TimeInterval = 7 * 24 * 60 * 60 - 60

    /// Returns `nil` when the window is already over (the caller should shield).
    public static func make(now: Date, lockAt: Date) -> MonitorPlan? {
        let remaining = lockAt.timeIntervalSince(now)
        guard remaining > UnlockPolicy.endTolerance else { return nil }
        if remaining >= minimumInterval {
            return MonitorPlan(start: now, end: min(lockAt, now.addingTimeInterval(maximumInterval)), warningMinutes: nil)
        }
        let shortfall = minimumInterval - remaining
        let extraMinutes = Int((shortfall / 60).rounded(.up))
        return MonitorPlan(start: now, end: lockAt.addingTimeInterval(Double(extraMinutes * 60)), warningMinutes: extraMinutes)
    }

    /// How long after the window the backstop schedule ends.
    public static let backstopDelay: TimeInterval = 60

    /// A second schedule ending a minute after `lockAt` (or after the minimum
    /// interval), with no warning: its `intervalDidEnd` reconciles again in case the
    /// grant schedule's callback never came.
    public static func backstop(now: Date, lockAt: Date) -> MonitorPlan {
        let end = max(lockAt.addingTimeInterval(backstopDelay), now.addingTimeInterval(minimumInterval))
        return MonitorPlan(start: now, end: min(end, now.addingTimeInterval(maximumInterval)), warningMinutes: nil)
    }

    static let components: Set<Calendar.Component> = [.era, .year, .month, .day, .hour, .minute, .second]

    /// Components carry `.timeZone`, so they name an absolute instant: a later
    /// time-zone change can't move the schedule (if the system honours the zone).
    public func startComponents(calendar: Calendar = .current) -> DateComponents {
        Self.absoluteComponents(start, calendar: calendar)
    }

    public func endComponents(calendar: Calendar = .current) -> DateComponents {
        Self.absoluteComponents(end, calendar: calendar)
    }

    static func absoluteComponents(_ date: Date, calendar: Calendar) -> DateComponents {
        var components = calendar.dateComponents(Self.components, from: date)
        components.timeZone = calendar.timeZone
        return components
    }

    public var warningComponents: DateComponents? {
        warningMinutes.map { DateComponents(minute: $0) }
    }
}
