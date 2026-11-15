import Foundation

/// Chosen on first run. A kid device (the child's device) runs screen time and the
/// web app's kid pages; a parent device (a parent's phone) runs the web app with
/// lock-screen approvals and never touches Family Controls.
enum DeviceRole: String, Sendable {
    case kid
    case parent

    private static let key = "deviceRole.v1"

    static func load(_ defaults: UserDefaults = .standard) -> DeviceRole? {
        defaults.string(forKey: key).flatMap(DeviceRole.init(rawValue:))
    }

    static func save(_ role: DeviceRole?, _ defaults: UserDefaults = .standard) {
        defaults.set(role?.rawValue, forKey: key)
    }
}
