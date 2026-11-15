import AVFoundation
import SwiftUI
import UIKit

/// Pairing state and actions, owned by `ParentSetupView` so it survives the
/// `Form` rebuilding its rows. Presentation is driven from outside the `Form`:
/// a `.sheet` attached to a `Section` inside a `Form` inside the parent sheet
/// tore down the whole parent sheet (and the PIN gate) as the scanner appeared.
@MainActor
@Observable
final class PairingFlow {
    enum Sheet: String, Identifiable {
        case scanner, code
        var id: String { rawValue }
    }

    var deviceName = DeviceName.suggested()
    var sheet: Sheet?
    var confirmingForget = false
    private(set) var working = false
    private(set) var status: String?
    var error: String?

    /// Asks for camera access if needed; true when the scanner may open.
    /// Replaced in tests.
    var requestCameraAccess: @MainActor () async -> CameraAccess = PairingFlow.systemCameraAccess

    enum CameraAccess { case granted, denied, unavailable }

    static let cameraDeniedMessage = "OpenCozy can't use the camera. Allow it in Settings → Apps → OpenCozy, or enter the code instead."

    /// Camera permission is settled *before* the scanner is presented, so the
    /// system prompt appears over the setup screen, not over a half-presented sheet.
    func scan() async {
        error = nil
        switch await requestCameraAccess() {
        case .granted: sheet = .scanner
        case .denied: error = Self.cameraDeniedMessage
        case .unavailable: error = "This device can't scan codes. Enter the code instead."
        }
    }

    func enterCode() {
        error = nil
        sheet = .code
    }

    /// A QR payload from the scanner.
    func scanned(_ payload: String, model: AppModel, pairing: any DevicePairingAPI = HTTPDevicePairingAPI()) async {
        sheet = nil
        guard let input = PairingInput.parseQR(payload) else {
            error = AppModel.SetupError.invalidPairingCode.localizedDescription
            return
        }
        await pair(input, model: model, pairing: pairing)
    }

    func pair(_ input: PairingInput, model: AppModel, pairing: any DevicePairingAPI = HTTPDevicePairingAPI()) async {
        sheet = nil
        working = true
        status = nil
        error = nil
        defer { working = false }
        do {
            let info = try await model.pair(input, deviceName: deviceName, pairing: pairing)
            status = "Paired for \(info.device.childName ?? "a child")."
        } catch APIError.server(409, _) {
            error = "That code has expired or was already used. Make a new one in Manage → Devices."
        } catch APIError.transport {
            error = "Can't reach OpenCozy at \(input.origin.host() ?? "that address"). Is this device on the home network?"
        } catch let failure {
            error = failure.localizedDescription
        }
    }

    func forget(model: AppModel) {
        model.unpair()
        status = nil
    }

    static func systemCameraAccess() async -> CameraAccess {
        guard QRScannerView.isSupported else { return .unavailable }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: return .granted
        case .notDetermined: return await AVCaptureDevice.requestAccess(for: .video) ? .granted : .denied
        default: return .denied
        }
    }
}

/// Parent setup section: pair this device by scanning the QR code from
/// OpenCozy → Manage → Devices → Pair a device, or by typing the short code.
/// Presents nothing itself; see `PairingFlow`.
struct PairingSection: View {
    @Environment(AppModel.self) private var model
    @Bindable var flow: PairingFlow

    var body: some View {
        Section {
            if let info = model.device {
                LabeledContent("Device", value: info.device.name)
                LabeledContent("Child", value: info.device.childName ?? "Not assigned")
                LabeledContent("Household", value: info.household.name)
            } else if model.isPaired {
                LabeledContent("Status", value: "Paired")
            }
            if !model.isPaired {
                TextField("Device name", text: $flow.deviceName)
            }
            Button(model.isPaired ? "Pair again" : "Scan pairing code", systemImage: "qrcode.viewfinder") {
                Task { await flow.scan() }
            }
            .disabled(flow.working || !QRScannerView.isSupported)
            Button("Enter the code instead", systemImage: "keyboard") { flow.enterCode() }
                .disabled(flow.working)
            if model.isPaired {
                Button("Forget pairing", role: .destructive) { flow.confirmingForget = true }
            }
            if flow.working { ProgressView("Pairing…") }
        } header: {
            Text("OpenCozy")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if let status = flow.status { Text(status).foregroundStyle(.green) }
                if let error = flow.error { Text(error).foregroundStyle(.red) }
                Text("In OpenCozy on another screen, open Manage → Devices → Pair a device and choose the child. The code works once, for 5 minutes. No password or key is stored on this device.")
            }
        }
    }
}

extension View {
    /// The pairing sheets and the forget confirmation. Attach outside the `Form`.
    func pairingPresentation(_ flow: PairingFlow, model: AppModel) -> some View {
        @Bindable var flow = flow
        return sheet(item: $flow.sheet) { sheet in
            switch sheet {
            case .scanner:
                NavigationStack {
                    QRScannerView { payload in Task { await flow.scanned(payload, model: model) } }
                        .ignoresSafeArea()
                        .navigationTitle("Scan pairing code")
                        .navigationBarTitleDisplayMode(.inline)
                        .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { flow.sheet = nil } } }
                }
            case .code:
                CodeEntryView(initialOrigin: model.config.baseURL?.absoluteString ?? "") { input in
                    Task { await flow.pair(input, model: model) }
                }
            }
        }
        .confirmationDialog("Forget this pairing?", isPresented: $flow.confirmingForget, titleVisibility: .visible) {
            Button("Forget and lock", role: .destructive) { flow.forget(model: model) }
        } message: {
            Text("Chosen apps stay locked until this device is paired again. Also revoke it in OpenCozy under Manage → Devices.")
        }
    }
}

private struct CodeEntryView: View {
    let initialOrigin: String
    let submit: (PairingInput) -> Void
    @State private var origin = ""
    @State private var code = ""
    @Environment(\.dismiss) private var dismiss

    private var input: PairingInput? { PairingInput.parseCode(code, origin: origin) }

    var body: some View {
        NavigationStack {
            Form {
                Section("OpenCozy address") {
                    TextField("https://opencozy.example.org", text: $origin)
                        .keyboardType(.URL).textContentType(.URL)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                }
                Section {
                    TextField("ABCD-EFGH", text: $code)
                        .font(.title2.monospaced())
                        .textInputAutocapitalization(.characters).autocorrectionDisabled()
                } header: {
                    Text("Pairing code")
                } footer: {
                    Text("The 8-character code under the QR code.")
                }
            }
            .navigationTitle("Enter code")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Pair") { if let input { submit(input) } }.disabled(input == nil)
                }
            }
            .onAppear { origin = initialOrigin }
        }
    }
}

