import DeviceActivity
import Foundation

/// Re-applies the shield when an unlock window ends.
///
/// Every callback re-derives the shield from the stored window instead of trusting
/// which callback fired, so a stale or duplicate callback can't unlock anything. An
/// end callback either shields or, if time genuinely remains (minute rounding, a
/// clock or time-zone quirk, a window longer than one schedule), arms a new schedule
/// for the remainder. It never leaves the shield off with nothing scheduled.
final class DeviceActivityMonitorExtension: DeviceActivityMonitor {
    private let lock = ScreenTimeLock()

    override func intervalDidStart(for activity: DeviceActivityName) {
        super.intervalDidStart(for: activity)
        if activity == .selfTest {
            lock.answerSelfTest()
            return
        }
        // Shields an expired window; never arms (see `LockDecision.Context.intervalStart`).
        lock.reconcile(context: .intervalStart)
    }

    /// Fires at the real end of a window shorter than Apple's 15-minute minimum interval
    /// (see `MonitorPlan`).
    override func intervalWillEndWarning(for activity: DeviceActivityName) {
        super.intervalWillEndWarning(for: activity)
        guard activity != .selfTest else { return }
        lock.reconcile(context: .intervalEnd)
    }

    override func intervalDidEnd(for activity: DeviceActivityName) {
        super.intervalDidEnd(for: activity)
        guard activity != .selfTest else { return }
        lock.reconcile(context: .intervalEnd)
    }
}
