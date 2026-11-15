import SwiftUI
import VisionKit

/// Live camera QR scanner (VisionKit DataScanner). Calls `onCode` once with the
/// first QR payload it reads. Scanning starts once the scanner is on screen
/// (`viewDidAppear`) and stops when it leaves, as VisionKit expects.
struct QRScannerView: UIViewControllerRepresentable {
    var onCode: (String) -> Void

    /// The hardware and OS can scan (camera permission is checked separately).
    static var isSupported: Bool { DataScannerViewController.isSupported }

    func makeUIViewController(context: Context) -> ScannerHost {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            qualityLevel: .balanced,
            recognizesMultipleItems: false,
            isHighFrameRateTrackingEnabled: false,
            isHighlightingEnabled: true
        )
        scanner.delegate = context.coordinator
        return ScannerHost(scanner: scanner)
    }

    func updateUIViewController(_ controller: ScannerHost, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(onCode: onCode) }

    /// Hosts the scanner as a child so scanning follows the appearance cycle.
    final class ScannerHost: UIViewController {
        let scanner: DataScannerViewController

        init(scanner: DataScannerViewController) {
            self.scanner = scanner
            super.init(nibName: nil, bundle: nil)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

        override func viewDidLoad() {
            super.viewDidLoad()
            view.backgroundColor = .black
            addChild(scanner)
            scanner.view.frame = view.bounds
            scanner.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            view.addSubview(scanner.view)
            scanner.didMove(toParent: self)
        }

        override func viewDidAppear(_ animated: Bool) {
            super.viewDidAppear(animated)
            if !scanner.isScanning { try? scanner.startScanning() }
        }

        override func viewWillDisappear(_ animated: Bool) {
            super.viewWillDisappear(animated)
            scanner.stopScanning()
        }
    }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        let onCode: (String) -> Void
        private var delivered = false

        init(onCode: @escaping (String) -> Void) { self.onCode = onCode }

        func dataScanner(_ scanner: DataScannerViewController, didAdd items: [RecognizedItem], allItems: [RecognizedItem]) {
            guard !delivered else { return }
            for item in items {
                if case .barcode(let code) = item, let payload = code.payloadStringValue, !payload.isEmpty {
                    delivered = true
                    scanner.stopScanning()
                    onCode(payload)
                    return
                }
            }
        }
    }
}
