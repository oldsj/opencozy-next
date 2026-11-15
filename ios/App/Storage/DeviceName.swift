import Foundation
import UIKit

/// A name that tells this device apart in Manage → Devices.
///
/// Since iOS 16, `UIDevice.current.name` is the name the user set only with
/// Apple's `user-assigned-device-name` entitlement, which this app doesn't have;
/// otherwise it's the generic model ("iPhone"), so every phone would look the
/// same. Then the suggestion adds the hardware model and the date, for example
/// "iPhone (iPhone17,3), 7 Nov 2026". Parents can change it in the bell sheet.
enum DeviceName {
    @MainActor
    static func suggested(now: Date = Date()) -> String {
        suggested(name: UIDevice.current.name, model: UIDevice.current.model, machine: machineIdentifier(), now: now)
    }

    static func suggested(name: String, model: String, machine: String, now: Date) -> String {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if !name.isEmpty, name.caseInsensitiveCompare(model) != .orderedSame { return String(name.prefix(80)) }
        let date = now.formatted(.dateTime.day().month(.abbreviated).year())
        let hardware = machine.isEmpty || machine == model ? "" : " (\(machine))"
        return String("\(model)\(hardware), \(date)".prefix(80))
    }

    /// "iPhone17,3"; in the simulator, the simulated model.
    static func machineIdentifier() -> String {
        if let simulated = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] { return simulated }
        var info = utsname()
        uname(&info)
        return withUnsafeBytes(of: &info.machine) { raw in
            String(decoding: raw.prefix { $0 != 0 }, as: UTF8.self)
        }
    }
}
