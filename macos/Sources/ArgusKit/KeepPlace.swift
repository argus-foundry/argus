import Foundation

/// Client-side half of kick-timing-keep-place for the macOS terminal.
///
/// The daemon defers the size-drift kill + resume while the user is scrolled up
/// or has unsent composer text, but it cannot see SwiftTerm's scroll position or
/// what was typed through this client; ``ArgusClient/resize`` reports both and
/// these pure helpers compute them. They mirror the TUI's `agentview.ComposerTyped`
/// byte for byte (a false positive only delays a width re-render; a false
/// negative would lose input).
public enum KeepPlace {

    /// How often a deferred resize is re-sent while the pane stays scrolled up
    /// or the draft stays unsent.
    public static let retryInterval: Duration = .seconds(10)

    /// Folds one chunk of genuine user input into the "composer may hold unsent
    /// text" flag. Printable bytes and bracketed-paste content set it; Enter
    /// (CR), Ctrl+C and Ctrl+U clear it; escape sequences (arrows, SGR mouse,
    /// focus, SS3, Alt+key), other control bytes and DEL are ignored — DEL
    /// cannot prove the box is empty again, so typed-then-erased stays flagged
    /// until the next submit.
    public static func composerTyped(_ typedIn: Bool, _ bytes: [UInt8]) -> Bool {
        var typed = typedIn
        var pasting = false
        var i = 0
        while i < bytes.count {
            let b = bytes[i]
            if b == 0x1b {
                i = skipEscape(bytes, i, &pasting)
            } else if pasting {
                typed = true
            } else if b == 0x0d || b == 0x03 || b == 0x15 {
                typed = false
            } else if b < 0x20 || b == 0x7f {
                // other control bytes: no effect
            } else {
                typed = true
            }
            i += 1
        }
        return typed
    }

    /// Index of the last byte of the escape sequence starting at `i`.
    /// Bracketed-paste start/end toggle `pasting`.
    private static func skipEscape(_ p: [UInt8], _ i: Int, _ pasting: inout Bool) -> Int {
        guard i + 1 < p.count else { return i }
        switch p[i + 1] {
        case UInt8(ascii: "["):
            var j = i + 2
            while j < p.count, p[j] >= 0x20, p[j] <= 0x3f { j += 1 }
            if j >= p.count { return p.count - 1 }
            let seq = Array(p[i...j])
            if seq == Array("\u{1b}[200~".utf8) {
                pasting = true
            } else if seq == Array("\u{1b}[201~".utf8) {
                pasting = false
            }
            return j
        case UInt8(ascii: "O"):
            return i + 2 < p.count ? i + 2 : i + 1
        default:
            return i + 1 // Alt+key: ESC plus one byte
        }
    }

    /// True when the viewport sits above the bottom of a scrollable buffer.
    /// `position` is SwiftTerm's 0...1 scroll position (1 = bottom). A buffer
    /// that cannot scroll (alt screen, no scrollback) is never "scrolled up".
    public static func isScrolledUp(position: Double, canScroll: Bool) -> Bool {
        canScroll && position < 0.999
    }

    /// Whether a deferred resize should keep being retried: only while the
    /// daemon still reports a deferral and the terminal view is attached.
    public static func shouldKeepRetrying(deferred: String, attached: Bool) -> Bool {
        attached && !deferred.isEmpty
    }
}
