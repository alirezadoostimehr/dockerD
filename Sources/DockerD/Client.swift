import Foundation
import Darwin

struct ServiceError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// The control channel never opens a TCP port or accepts browser requests.
struct ServiceClient: Sendable {
    let socketPath: String

    func request<T: Decodable & Sendable>(_ method: String = "GET", _ path: String, body: Data? = nil, as: T.Type = T.self) async throws -> T {
        let data = try await Task.detached(priority: .userInitiated) {
            try exchange(method, path, body: body ?? Data())
        }.value
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func exchange(_ method: String, _ path: String, body: Data) throws -> Data {
        guard socketPath.utf8.count < 104, !path.contains("\r"), !path.contains("\n") else {
            throw ServiceError(message: "Invalid local service address")
        }
        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw systemError() }
        defer { Darwin.close(fd) }
        var timeout = timeval(tv_sec: 900, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        var noSignal: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &noSignal, socklen_t(MemoryLayout.size(ofValue: noSignal)))
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.copyBytes(from: Array(socketPath.utf8) + [0])
        }
        let connected = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard connected == 0 else { throw systemError() }
        var request = Data("\(method) \(path) HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: \(body.count)\r\n\r\n".utf8)
        request.append(body)
        try request.withUnsafeBytes { pointer in
            var sent = 0
            while sent < request.count {
                let count = Darwin.write(fd, pointer.baseAddress!.advanced(by: sent), request.count - sent)
                if count < 0 && errno == EINTR { continue }
                guard count > 0 else { throw systemError() }
                sent += count
            }
        }
        var response = Data()
        var buffer = [UInt8](repeating: 0, count: 16384)
        while true {
            let count = Darwin.read(fd, &buffer, buffer.count)
            if count < 0 && errno == EINTR { continue }
            guard count >= 0 else { throw systemError() }
            if count == 0 { break }
            response.append(contentsOf: buffer.prefix(count))
            guard response.count <= 40 * 1024 * 1024 else { throw ServiceError(message: "The service response is too large") }
        }
        return try Self.decodeHTTP(response)
    }

    static func decodeHTTP(_ data: Data) throws -> Data {
        guard let separator = data.range(of: Data("\r\n\r\n".utf8)),
              let headers = String(data: data[..<separator.lowerBound], encoding: .utf8),
              let first = headers.components(separatedBy: "\r\n").first,
              let code = Int(first.split(separator: " ").dropFirst().first ?? "") else {
            throw ServiceError(message: "The service returned an invalid HTTP response")
        }
        var body = Data(data[separator.upperBound...])
        if headers.lowercased().contains("transfer-encoding: chunked") {
            var decoded = Data()
            while true {
                guard let line = body.range(of: Data("\r\n".utf8)),
                      let lengthText = String(data: body[..<line.lowerBound], encoding: .utf8)?.split(separator: ";").first,
                      let length = Int(lengthText, radix: 16), length >= 0 else { throw ServiceError(message: "Invalid chunked response") }
                body = Data(body[line.upperBound...])
                if length == 0 { break }
                guard body.count >= length + 2, body[length] == 13, body[length + 1] == 10 else { throw ServiceError(message: "Incomplete service response") }
                decoded.append(body.prefix(length))
                body = Data(body.dropFirst(length + 2))
            }
            body = decoded
        }
        guard (200..<300).contains(code) else {
            let error = try? JSONDecoder().decode([String: String].self, from: body)
            throw ServiceError(message: error?["error"] ?? "Service request failed (\(code))")
        }
        return body
    }

    private func systemError() -> ServiceError {
        ServiceError(message: "Cannot reach the DockerD service: \(String(cString: strerror(errno)))")
    }
}
