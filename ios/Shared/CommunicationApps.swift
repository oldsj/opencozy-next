import Foundation

/// Communication apps are never shielded: Messages, FaceTime, Phone and Contacts.
///
/// Family Controls tokens are opaque. In the app and the monitor extension an
/// `ApplicationToken` can't be turned into a bundle identifier, and a token can't
/// be made from one. So these apps are identified two ways:
///
/// 1. **Always allowed (guaranteed for what's listed):** the parent picks them in
///    a second `FamilyActivityPicker`. Their tokens are subtracted from the locked
///    apps and passed as the `except:` set of the category shield, so a locked
///    category (Social, say) never covers them.
/// 2. **Detected (best effort):** the system hands the shield-configuration
///    extension the `Application` it is about to cover, with its bundle identifier.
///    If that's one of `bundleIdentifiers`, the extension records the token as
///    always allowed and lifts its shield straight away (see
///    `ShieldConfigurationExtension`). This runs only once the app has been shielded
///    and opened, and depends on the extension being allowed to write.
public enum CommunicationApps {
    /// Messages, FaceTime, Phone, Contacts.
    public static let bundleIdentifiers: Set<String> = [
        "com.apple.mobilesms", "com.apple.facetime", "com.apple.mobilephone", "com.apple.mobileaddressbook",
    ]

    public static func isCommunication(bundleIdentifier: String?) -> Bool {
        guard let bundleIdentifier else { return false }
        return bundleIdentifiers.contains(bundleIdentifier.lowercased())
    }

    /// Shown in the picker flow.
    public static let pickerNote = "Messages and FaceTime are always allowed; set who they can contact in Screen Time → Communication Limits."
}

/// What to shield, with always-allowed apps taken out. Generic so it's tested
/// without Family Controls tokens (which can't be created outside the picker).
public struct ShieldPlan<App: Hashable, Category: Hashable>: Equatable {
    /// Individually locked apps, without the always-allowed ones. `nil` = none.
    public var applications: Set<App>?
    /// Locked categories. `nil` = none.
    public var categories: Set<Category>?
    /// Apps exempt from the category shield.
    public var categoryExceptions: Set<App>

    public static func make(applications: Set<App>, categories: Set<Category>, alwaysAllowed: Set<App>) -> ShieldPlan {
        let locked = applications.subtracting(alwaysAllowed)
        return ShieldPlan(applications: locked.isEmpty ? nil : locked,
                          categories: categories.isEmpty ? nil : categories,
                          categoryExceptions: categories.isEmpty ? [] : alwaysAllowed)
    }
}
