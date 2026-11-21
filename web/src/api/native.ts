// The iPad app's bridge (ios/README.md): the kid view runs in the app's web view, and the app
// re-syncs its shields when the page says the child's iPad time changed. Outside the app there
// is no handler and this does nothing. The server also wakes the app with a push; this is the
// instant path, without waiting for APNs.
type Bridge = { webkit?: { messageHandlers?: { opencozy?: { postMessage(message: unknown): void } } } };

export function screenTimeChanged(w: Bridge = window as Bridge) {
  try {
    w.webkit?.messageHandlers?.opencozy?.postMessage({ type: 'screenTimeChanged' });
  } catch {
    // The app's handler went away: the push and the app's own sync still catch up.
  }
}
