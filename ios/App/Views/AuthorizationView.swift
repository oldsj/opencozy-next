import FamilyControls
import SwiftUI

/// First-run step: Screen Time (Family Controls) permission for a child account.
/// A parent approves on this device with their own Apple Account credentials.
struct AuthorizationView: View {
    @Environment(AppModel.self) private var model
    @State private var requesting = false

    var body: some View {
        ContentUnavailableView {
            Label("Screen Time permission", systemImage: "hourglass")
        } description: {
            VStack(spacing: 12) {
                Text("OpenCozy locks chosen apps until points are spent on screen time. A parent in this device's Family Sharing group needs to approve.")
                if model.authorizationStatus == .denied {
                    Text("Permission was declined. Ask a parent to try again.")
                        .foregroundStyle(.orange)
                }
                if let error = model.errorMessage {
                    Text(error).foregroundStyle(.orange)
                }
            }
        } actions: {
            Button {
                requesting = true
                Task {
                    await model.requestAuthorization()
                    requesting = false
                    await model.refresh()
                }
            } label: {
                if requesting { ProgressView() } else { Text("Ask a parent to approve") }
            }
            .buttonStyle(.borderedProminent)
            .disabled(requesting)
        }
    }
}
