import Foundation

struct EngineSettings: Codable, Equatable, Sendable {
    var cpus = 4
    var memoryGiB = 4
    var diskGiB = 60
    var kubernetes = true
    var sharedFolders: [String] = []
    var idleEnabled = true
    var idleMinutes = 10
    var countdownSeconds = 60
}
struct Blocker: Codable, Identifiable, Sendable {
    var kind: String
    var name: String
    var detail: String
    var id: String { kind + name + detail }
    var symbol: String {
        switch kind { case "container": "shippingbox"; case "pod": "cube.transparent"; case "exec", "attach": "terminal"; case "portforward": "arrow.left.arrow.right"; case "monitor": "eye.slash"; default: "gearshape.2" }
    }
}
struct Activity: Codable, Sendable {
    var state: String
    var blockers: [Blocker]
    var healthy: Bool
    var idleSince: String?
    var deadline: String?
    var deadlineDate: Date? {
        guard let deadline else { return nil }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: deadline) ?? ISO8601DateFormatter().date(from: deadline)
    }
}
struct EngineStatus: Codable, Sendable {
    var state: String
    var message: String
    var error: String?
    var runtimeInstalled: Bool
    var vmExists: Bool
    var settings: EngineSettings
    var activity: Activity
    var home: String
    var kubeconfig: String
    var running: Bool { state == "active" || state == "idle countdown" }
    var changing: Bool { state == "starting" || state == "stopping" }
}
struct Resource: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var subtitle: String
    var state: String
    var group: String
    var namespace: String
    var size: Int64
    var ports: String
    var active: Bool { ["running", "Running", "restarting", "Pending"].contains(state) }
    var identity: String { namespace + "/" + id }
}
struct PortForward: Codable, Identifiable, Sendable {
    var id: String
    var pod: String
    var namespace: String
    var localPort: Int
    var remotePort: Int
    var state: String
    var error: String?
}
struct TextResponse: Codable, Sendable { var text: String }
struct ActionResponse: Codable, Sendable { var text: String?; var path: String? }
struct Acknowledgment: Codable, Sendable { var ok: Bool?; var accepted: Bool?; var id: String? }
enum Section: String, CaseIterable, Identifiable {
    case overview = "Overview", containers = "Containers", images = "Images", volumes = "Volumes", pods = "Kubernetes", forwards = "Port forwards", settings = "Settings"
    var id: String { rawValue }
    var key: String { self == .pods ? "pods" : rawValue.lowercased() }
    var symbol: String {
        switch self { case .overview: "square.grid.2x2"; case .containers: "shippingbox"; case .images: "square.stack.3d.up"; case .volumes: "externaldrive"; case .pods: "helm"; case .forwards: "arrow.left.arrow.right"; case .settings: "slider.horizontal.3" }
    }
    var subtitle: String {
        switch self { case .overview: "Your local development environment, at a glance."; case .containers: "Run, inspect, and manage your containers."; case .images: "The building blocks of your environment."; case .volumes: "Persistent storage for your applications."; case .pods: "A local cluster, ready for your next idea."; case .forwards: "Secure connections from localhost to your pods."; case .settings: "Make DockerD work the way you do." }
    }
}
