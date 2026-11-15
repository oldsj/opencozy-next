import Foundation
import SwiftUI
import Testing
@testable import OpenCozy

// Pairing scanner on a device: the parent sheet (and its PIN unlock) vanished as
// the scanner appeared. Presentation now lives outside the Form, the unlock lives
// above the sheet, and camera permission is settled before the scanner opens.

@MainActor
struct ParentGateTests {
    final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }

    @Test func cameraAndPermissionPromptsDontRelock() {
        let gate = ParentGate()
        gate.open()
        gate.unlock()
        // A system prompt bounces the scene through inactive and back.
        gate.scenePhaseChanged(.inactive)
        gate.scenePhaseChanged(.active)
        #expect(gate.isUnlocked)
        #expect(gate.isPresented)
    }

    @Test func backgroundLocks() {
        let gate = ParentGate()
        gate.open()
        gate.unlock()
        gate.scenePhaseChanged(.background)
        #expect(!gate.isUnlocked)
    }

    @Test func doneClosesAndLocks() {
        let gate = ParentGate()
        gate.open()
        gate.unlock()
        gate.close()
        #expect(!gate.isPresented)
        #expect(!gate.isUnlocked)
        gate.open()
        #expect(gate.isPresented && !gate.isUnlocked)
    }

    @Test func aRebuiltSheetKeepsTheUnlockBriefly() {
        let clock = Clock()
        let gate = ParentGate(now: { clock.now })
        gate.open()
        gate.unlock()
        // The sheet goes away without Done (system dismissal) and is reopened.
        gate.isPresented = false
        clock.now += 60
        gate.open()
        #expect(gate.isUnlocked)
        gate.isPresented = false
        clock.now += ParentGate.reopenGrace
        gate.open()
        #expect(!gate.isUnlocked)
    }
}

@MainActor
struct PairingFlowStateTests {
    private func model() -> AppModel {
        let unique = "org.opencozy.tests.\(UUID().uuidString)"
        let config = AppConfig(keys: DeviceKeyStore(keychain: Keychain(service: unique)), clock: DeviceClock(defaultsSuite: unique), defaultsSuite: unique)
        return AppModel(preview: FixtureAPI(), config: config)
    }

    private func flow(_ access: PairingFlow.CameraAccess) -> PairingFlow {
        let flow = PairingFlow()
        flow.requestCameraAccess = { access }
        return flow
    }

    @Test func scannerOpensOnlyAfterCameraAccess() async {
        let granted = flow(.granted)
        await granted.scan()
        #expect(granted.sheet == .scanner)
        #expect(granted.error == nil)

        let denied = flow(.denied)
        await denied.scan()
        #expect(denied.sheet == nil)
        #expect(denied.error == PairingFlow.cameraDeniedMessage)

        let unsupported = flow(.unavailable)
        await unsupported.scan()
        #expect(unsupported.sheet == nil)
        #expect(unsupported.error != nil)
    }

    @Test func aScannedCodePairsAndClosesTheScanner() async throws {
        let flow = flow(.granted)
        await flow.scan()
        let recorder = PairingFlowTests.Recorder()
        let model = model()
        let token = String(repeating: "x", count: 43)
        await flow.scanned("opencozy-pair:v1?origin=https%3A%2F%2Fopencozy.example.org&token=\(token)", model: model,
                           pairing: PairingFlowTests.FakePairing(recorder: recorder))
        #expect(flow.sheet == nil)
        #expect(!flow.working)
        #expect(flow.error == nil)
        #expect(flow.status?.hasPrefix("Paired for") == true)
        #expect(await recorder.requests.first?.1.token == token)
        #expect(model.config.baseURL == URL(string: "https://opencozy.example.org"))
    }

    @Test func aForeignQRCodeIsExplained() async {
        let flow = flow(.granted)
        await flow.scan()
        let recorder = PairingFlowTests.Recorder()
        await flow.scanned("https://example.org/not-a-pairing-code", model: model(), pairing: PairingFlowTests.FakePairing(recorder: recorder))
        #expect(flow.sheet == nil)
        #expect(flow.error == AppModel.SetupError.invalidPairingCode.localizedDescription)
        #expect(await recorder.requests.isEmpty)
    }

    @Test func anExpiredCodeIsExplained() async {
        let flow = flow(.granted)
        let recorder = PairingFlowTests.Recorder()
        await recorder.fail(.server(status: 409, message: "Pairing unavailable."))
        await flow.pair(.code(origin: URL(string: "https://opencozy.example.org")!, code: "ABCD-EFGH"), model: model(),
                        pairing: PairingFlowTests.FakePairing(recorder: recorder))
        #expect(flow.error?.contains("expired") == true)
        #expect(flow.status == nil)
    }
}
