import Foundation

extension ArgusClient {
    /// `GET /api/tasks/{id}/output` — the most recent terminal output.
    /// `tailBytes` caps the body (pass nil for the server default, 32 KiB);
    /// `clean` strips ANSI control sequences. The returned ``OutputTail/total``
    /// (from `X-Output-Total`) is the `since` cursor for a gapless
    /// ``terminalStream(taskID:since:expectedPID:)`` alongside the process ID.
    /// A pending replacement is retried at a bounded cadence until the owner settles
    /// or the task is cancelled.
    public func output(taskID: String, tailBytes: Int? = nil,
                       clean: Bool = false) async throws -> OutputTail {
        var q: [URLQueryItem] = []
        if let tailBytes, tailBytes > 0 { q.append(.init(name: "bytes", value: String(tailBytes))) }
        if clean { q.append(.init(name: "clean", value: "1")) }
        let req = try makeRequest(method: "GET", path: "/api/tasks/\(pc(taskID))/output", query: q)
        let (data, http) = try await sendTerminalSnapshot(req)
        let total = UInt64(http.value(forHTTPHeaderField: "X-Output-Total") ?? "") ?? 0
        let source = http.value(forHTTPHeaderField: "X-Source") ?? ""
        let pid = Int(http.value(forHTTPHeaderField: "X-Argus-Session-Pid") ?? "")
        return OutputTail(data: data, total: total, source: source, sessionPID: pid)
    }

    private func sendTerminalSnapshot(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        var delay: Duration = .milliseconds(150)
        while true {
            try _Concurrency.Task.checkCancellation()
            do {
                return try await send(request)
            } catch let error as ArgusError where error.isStaleSession {
                try await _Concurrency.Task.sleep(for: delay)
                delay = min(delay * 2, .seconds(1))
            }
        }
    }

    /// `POST /api/tasks/{id}/input` — writes raw bytes to the agent's PTY.
    /// The server caps a single write at 64 KiB; the caller must keep `data`
    /// within that bound.
    public func sendInput(taskID: String, _ data: Data) async throws {
        let req = try makeRequest(method: "POST", path: "/api/tasks/\(pc(taskID))/input",
                                  body: data, contentType: "application/octet-stream")
        _ = try await send(req)
    }

    /// `GET /api/tasks/{id}/size` — the active PTY dimensions.
    public func terminalSize(taskID: String) async throws -> PTYSize {
        try await getDecoding("/api/tasks/\(pc(taskID))/size")
    }

    /// `POST /api/tasks/{id}/resize` — sends a SIGWINCH-equivalent resize.
    /// `scrolledUp` / `composerDirty` report client-side state the daemon cannot
    /// see so it can defer the size-drift kick (kick-timing-keep-place).
    public func resize(taskID: String, rows: UInt16, cols: UInt16,
                       scrolledUp: Bool = false, composerDirty: Bool = false) async throws -> ResizeResult {
        struct Body: Encodable {
            let rows: UInt16
            let cols: UInt16
            let scrolled_up: Bool
            let composer_dirty: Bool
        }
        return try await sendDecoding("POST", "/api/tasks/\(pc(taskID))/resize",
                                      body: Body(rows: rows, cols: cols,
                                                 scrolled_up: scrolledUp, composer_dirty: composerDirty))
    }
}
