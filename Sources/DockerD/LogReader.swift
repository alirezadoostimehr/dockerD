import Foundation

enum LogSource: String, CaseIterable, Identifiable, Sendable {
  case service = "Service"
  case runtime = "Startup"
  case vm = "Virtual machine"
  case console = "Boot console"

  var id: String { rawValue }
  var path: String {
    switch self {
    case .service: "service.log"
    case .runtime: "runtime.log"
    case .vm: "lima/dockerd/ha.stderr.log"
    case .console: "lima/dockerd/serialv.log"
    }
  }
}

struct LogSnapshot: Sendable {
  var text: String
  var truncated: Bool
}

enum LogReader {
  static let limit = 128 * 1024

  // Read directly so diagnostics still work if the service cannot start. Seek
  // from EOF instead of loading an unbounded VM log into the UI's memory.
  static func read(_ url: URL) throws -> LogSnapshot {
    let file: FileHandle
    do {
      file = try FileHandle(forReadingFrom: url)
    } catch let error as CocoaError
      where error.code == .fileNoSuchFile || error.code == .fileReadNoSuchFile
    {
      return LogSnapshot(text: "", truncated: false)
    }
    defer { try? file.close() }
    let size = try file.seekToEnd()
    let truncated = size > UInt64(limit)
    try file.seek(toOffset: truncated ? size - UInt64(limit) : 0)
    var data = try file.read(upToCount: limit) ?? Data()
    if truncated, let newline = data.firstIndex(of: 10) {
      data = Data(data.dropFirst(newline + 1))
    }
    return LogSnapshot(text: String(decoding: data, as: UTF8.self), truncated: truncated)
  }
}
