import ManagedSettings
import ManagedSettingsUI
import UIKit

/// The screen a child sees when opening a locked app or website.
final class ShieldConfigurationExtension: ShieldConfigurationDataSource {
    override func configuration(shielding application: Application) -> ShieldConfiguration {
        communicationOverride(application) ?? locked(name: application.localizedDisplayName)
    }

    override func configuration(shielding application: Application, in category: ActivityCategory) -> ShieldConfiguration {
        communicationOverride(application) ?? locked(name: application.localizedDisplayName)
    }

    /// Messages, FaceTime, Phone and Contacts are never locked. Only here does the
    /// system reveal the bundle identifier behind a token, so this is where a
    /// communication app picked by mistake (or inside a locked category) is caught:
    /// its token is recorded as always allowed and its shield lifted at once. Best
    /// effort; the parent's Always allowed list is the guaranteed path.
    private func communicationOverride(_ application: Application) -> ShieldConfiguration? {
        guard CommunicationApps.isCommunication(bundleIdentifier: application.bundleIdentifier), let token = application.token else { return nil }
        ScreenTimeLock().exemptCommunicationApp(token)
        let name = application.localizedDisplayName ?? "This app"
        return ShieldConfiguration(
            backgroundBlurStyle: .systemThickMaterial,
            backgroundColor: nil,
            icon: UIImage(systemName: "checkmark.circle"),
            title: .init(text: "\(name) is always allowed", color: .label),
            subtitle: .init(text: "Close this and open \(name) again.", color: .secondaryLabel),
            primaryButtonLabel: .init(text: "OK", color: .white),
            primaryButtonBackgroundColor: .systemGreen,
            secondaryButtonLabel: nil
        )
    }

    override func configuration(shielding webDomain: WebDomain) -> ShieldConfiguration {
        locked(name: webDomain.domain)
    }

    override func configuration(shielding webDomain: WebDomain, in category: ActivityCategory) -> ShieldConfiguration {
        locked(name: webDomain.domain)
    }

    private func locked(name: String?) -> ShieldConfiguration {
        let title = name.map { "\($0) is locked" } ?? "Locked"
        let store = SharedStore(), content = store.shieldContent, queued = store.queuedTimeRequest != nil
        return ShieldConfiguration(
            backgroundBlurStyle: .systemThickMaterial,
            backgroundColor: nil,
            icon: UIImage(systemName: "hourglass"),
            title: .init(text: title, color: .label),
            subtitle: .init(text: ShieldText.subtitle(content, now: Date(), queued: queued, canOpenApp: ShieldCapabilities.canOpenApp),
                            color: .secondaryLabel),
            primaryButtonLabel: .init(text: OneMoreMinute.isAvailable(store: store) ? "One more minute" : "OK", color: .white),
            primaryButtonBackgroundColor: .systemIndigo,
            secondaryButtonLabel: .init(text: ShieldText.askButton(content?.ask, queued: queued), color: .systemIndigo)
        )
    }
}

/// Mirrors the ShieldAction extension's capability check. The shield-action response
/// that opens the parental controls app needs iOS 26.5.
enum ShieldCapabilities {
    static var canOpenApp: Bool {
        if #available(iOS 26.5, *) { return true }
        return false
    }
}
