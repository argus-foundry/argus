import Foundation
import Testing
@testable import ArgusKit

/// The keep-place helpers mirror the TUI's `agentview.ComposerTyped` and the
/// daemon's resize `deferred` contract (kick-timing-keep-place-clients).
@Suite("Keep place")
struct KeepPlaceTests {

    private func fold(_ typed: Bool, _ s: String) -> Bool {
        KeepPlace.composerTyped(typed, [UInt8](s.utf8))
    }

    @Test("printable bytes set the flag; Enter clears it")
    func typedThenSubmitted() {
        #expect(fold(false, "fix") == true)
        #expect(fold(true, "\r") == false)
        #expect(fold(false, "fix\r") == false)   // submitted in the same chunk
        #expect(fold(false, "a\rb") == true)     // typing after the submit
    }

    @Test("Ctrl+C and Ctrl+U clear the flag")
    func clearingControls() {
        #expect(fold(true, "\u{03}") == false)
        #expect(fold(true, "\u{15}") == false)
    }

    @Test("DEL and other control bytes cannot prove the box is empty")
    func ignoredControls() {
        #expect(fold(true, "\u{7f}") == true)
        #expect(fold(true, "\u{08}") == true)
        #expect(fold(false, "\u{7f}") == false)
    }

    @Test("escape sequences are ignored: arrows, SGR mouse, focus, SS3, Alt+key")
    func escapesIgnored() {
        #expect(fold(false, "\u{1b}[A") == false)
        #expect(fold(false, "\u{1b}[<0;10;5M") == false)
        #expect(fold(false, "\u{1b}[I") == false)
        #expect(fold(false, "\u{1b}OP") == false)
        #expect(fold(false, "\u{1b}b") == false)
        #expect(fold(true, "\u{1b}[A") == true)
        #expect(fold(true, "\u{1b}[1;5C\r") == false)
    }

    @Test("bracketed paste content sets the flag, including bytes that look like control keys")
    func bracketedPaste() {
        #expect(fold(false, "\u{1b}[200~hello\u{1b}[201~") == true)
        #expect(fold(false, "\u{1b}[200~\r\u{1b}[201~") == true)   // a pasted newline is content
        #expect(fold(true, "\u{1b}[200~x\u{1b}[201~\r") == false)  // then Enter submits
    }

    @Test("a truncated escape at the end of a chunk is tolerated")
    func truncatedEscape() {
        #expect(fold(false, "\u{1b}") == false)
        #expect(fold(false, "\u{1b}[") == false)
        #expect(fold(true, "\u{1b}[1;") == true)
    }

    @Test("multi-byte UTF-8 counts as typed text")
    func utf8() {
        #expect(fold(false, "héllo") == true)
        #expect(fold(false, "日本") == true)
    }

    @Test("scrolled-up requires a scrollable buffer above the bottom")
    func scrolledUp() {
        #expect(KeepPlace.isScrolledUp(position: 0.4, canScroll: true) == true)
        #expect(KeepPlace.isScrolledUp(position: 1.0, canScroll: true) == false)
        #expect(KeepPlace.isScrolledUp(position: 0.9995, canScroll: true) == false)
        #expect(KeepPlace.isScrolledUp(position: 0.0, canScroll: false) == false) // alt screen / no scrollback
    }

    @Test("retry continues only while deferred and the view is attached")
    func retryPolicy() {
        #expect(KeepPlace.shouldKeepRetrying(deferred: "scrolled", attached: true) == true)
        #expect(KeepPlace.shouldKeepRetrying(deferred: "draft", attached: true) == true)
        #expect(KeepPlace.shouldKeepRetrying(deferred: "", attached: true) == false)
        #expect(KeepPlace.shouldKeepRetrying(deferred: "draft", attached: false) == false)
        #expect(KeepPlace.shouldKeepRetrying(deferred: "scrolled", attached: false) == false)
    }

    @Test("retry interval is ten seconds")
    func interval() {
        #expect(KeepPlace.retryInterval == .seconds(10))
    }
}
