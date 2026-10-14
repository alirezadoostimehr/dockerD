import AppKit
import SwiftUI
import UserNotifications

@MainActor
final class AppModel: ObservableObject {
  @Published var status: EngineStatus?
  @Published var resources: [Resource] = []
  @Published var forwards: [PortForward] = []
  @Published var section: Section = .overview {
    didSet {
      selected = nil
      search = ""
      namespace = "All namespaces"
      Task { await refreshResources() }
    }
  }
  @Published var search = ""
  @Published var namespace = "All namespaces"
  @Published var selected: Resource?
  @Published var error: String?
  @Published var busy = false
  @Published var loading = false
  @Published var connected = false
  @Published var detail = ""
  @Published var detailView = "inspect"
  @Published var diagnostics: String?
  @Published var toast: String?
  let client: ServiceClient
  let home: URL
  private var poll: Task<Void, Never>?
  private var service: Process?
  private var wakeObserver: NSObjectProtocol?
  private var loadingResources = false

  init() {
    home = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(
      "Library/Application Support/DockerD")
    client = ServiceClient(socketPath: home.appendingPathComponent("control.sock").path)
    wakeObserver = NSWorkspace.shared.notificationCenter.addObserver(
      forName: NSWorkspace.didWakeNotification, object: nil, queue: .main
    ) { [weak self] _ in
      Task { @MainActor in await self?.resetIdle() }
    }
    poll = Task {
      await connect()
      while !Task.isCancelled {
        await refresh()
        try? await Task.sleep(for: .seconds(3))
      }
    }
  }

  func connect() async {
    if let current = try? await client.request("GET", "/v1/status", as: EngineStatus.self) {
      status = current
      connected = true
      return
    }
    do {
      try FileManager.default.createDirectory(
        at: home, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
      let resources = Bundle.main.resourceURL!
      let executable = Bundle.main.bundleURL.appendingPathComponent(
        "Contents/MacOS/dockerd-service")
      guard FileManager.default.isExecutableFile(atPath: executable.path) else {
        throw ServiceError(
          message: "Build the complete app with ‘make app’, then open dist/DockerD.app.")
      }
      let process = Process()
      process.executableURL = executable
      process.arguments = [
        "--home", home.path, "--bundle", resources.appendingPathComponent("Runtime").path,
        "--resources", resources.path,
      ]
      let log = home.appendingPathComponent("service.log")
      if !FileManager.default.fileExists(atPath: log.path) {
        FileManager.default.createFile(
          atPath: log.path, contents: nil, attributes: [.posixPermissions: 0o600])
      }
      let handle = try FileHandle(forWritingTo: log)
      try handle.seekToEnd()
      process.standardOutput = handle
      process.standardError = handle
      try process.run()
      service = process
      for _ in 0..<30 {
        if let current = try? await client.request("GET", "/v1/status", as: EngineStatus.self) {
          status = current
          connected = true
          return
        }
        try await Task.sleep(for: .milliseconds(200))
      }
      throw ServiceError(message: "The service did not start. Check service.log in \(home.path).")
    } catch { self.error = error.localizedDescription }
  }

  func refresh() async {
    do {
      let updated = try await client.request("GET", "/v1/status", as: EngineStatus.self)
      if updated.state == "idle countdown" && status?.state != "idle countdown" {
        notify("Engine will stop soon", "DockerD is idle. Open the app to keep it running.")
      }
      if updated.state == "stopped" && status?.state == "stopping" {
        notify("Engine stopped", "Your containers, images, and volumes are preserved.")
      }
      status = updated
      connected = true
      if updated.running {
        await refreshResources()
      } else {
        resources = []
        forwards = []
      }
    } catch { connected = false }
  }

  func refreshResources() async {
    guard status?.running == true, !loadingResources else { return }
    let current = section
    loadingResources = true
    defer {
      loadingResources = false
      loading = false
    }
    do {
      if current == .forwards {
        let values = try await client.request("GET", "/v1/forwards", as: [PortForward].self)
        if section == current { forwards = values }
      } else if [.containers, .images, .volumes, .pods].contains(current) {
        if current == .pods && status?.settings.kubernetes == false {
          resources = []
          return
        }
        let values = try await client.request(
          "GET", "/v1/resources/\(current.key)", as: [Resource].self)
        if section == current { resources = values }
      }
    } catch { if section == current { self.error = error.localizedDescription } }
  }

  var filtered: [Resource] {
    resources.filter {
      (search.isEmpty
        || "\($0.name) \($0.subtitle) \($0.group)".localizedCaseInsensitiveContains(search))
        && (namespace == "All namespaces" || $0.namespace == namespace)
    }
  }
  var namespaces: [String] {
    ["All namespaces"] + Set(resources.map(\.namespace).filter { !$0.isEmpty }).sorted()
  }

  func engine(_ action: String) async {
    do {
      let _: Acknowledgment = try await client.request("POST", "/v1/\(action)")
      await refresh()
    } catch { self.error = error.localizedDescription }
  }
  func resetIdle() async {
    do {
      let _: Acknowledgment = try await client.request("POST", "/v1/idle/reset")
      await refresh()
    } catch { self.error = error.localizedDescription }
  }
  func save(_ settings: EngineSettings) async {
    do {
      let _: EngineSettings = try await client.request(
        "PUT", "/v1/settings", body: JSONEncoder().encode(settings))
      await refresh()
      showToast("Settings saved")
    } catch { self.error = error.localizedDescription }
  }
  func activateContext() async {
    do {
      let _: Acknowledgment = try await client.request("POST", "/v1/context/activate")
      showToast("Docker context switched to dockerd-local")
    } catch { self.error = error.localizedDescription }
  }
  func installCLI() async {
    do {
      let _: Acknowledgment = try await client.request("POST", "/v1/cli/install")
      showToast("CLI tools and Compose/Buildx links installed")
    } catch { self.error = error.localizedDescription }
  }
  func runImage(_ resource: Resource, name: String, local: Int, container: Int) async {
    busy = true
    defer { busy = false }
    do {
      let body = try JSONSerialization.data(withJSONObject: [
        "action": "run", "kind": "images", "id": resource.id, "value": name, "localPort": local,
        "containerPort": container,
      ])
      let _: ActionResponse = try await client.request("POST", "/v1/action", body: body)
      section = .containers
      await refreshResources()
      showToast("Container started")
    } catch { self.error = error.localizedDescription }
  }
  func action(_ name: String, resource: Resource? = nil, value: String = "", kind: String? = nil)
    async
  {
    busy = true
    defer { busy = false }
    do {
      let body = [
        "kind": kind ?? section.key, "id": resource?.id ?? "",
        "namespace": resource?.namespace ?? "", "action": name, "value": value,
      ]
      let result = try await client.request(
        "POST", "/v1/action", body: JSONEncoder().encode(body), as: ActionResponse.self)
      if let path = result.path { NSWorkspace.shared.open(URL(fileURLWithPath: path)) }
      if name == "remove" { selected = nil }
      await refreshResources()
      showToast(name == "terminal" ? "Terminal opened" : "Operation completed")
    } catch { self.error = error.localizedDescription }
  }
  func loadDetail(_ resource: Resource) async {
    let kind = section.key
    let view = detailView
    var query = URLComponents()
    query.path = "/v1/detail"
    query.queryItems = [
      URLQueryItem(name: "kind", value: kind), URLQueryItem(name: "id", value: resource.id),
      URLQueryItem(name: "namespace", value: resource.namespace),
      URLQueryItem(name: "view", value: view),
    ]
    do {
      let result = try await client.request("GET", query.string!, as: TextResponse.self)
      guard selected?.identity == resource.identity, detailView == view else { return }
      if view == "inspect", let data = result.text.data(using: .utf8),
        let object = try? JSONSerialization.jsonObject(with: data),
        let pretty = try? JSONSerialization.data(
          withJSONObject: object, options: [.prettyPrinted, .sortedKeys]),
        let text = String(data: pretty, encoding: .utf8)
      {
        detail = text
      } else {
        detail = result.text.isEmpty ? "No logs yet." : result.text
      }
    } catch { detail = error.localizedDescription }
  }
  func createForward(pod: String, namespace: String, local: Int, remote: Int) async {
    do {
      let data = try JSONSerialization.data(withJSONObject: [
        "pod": pod, "namespace": namespace, "localPort": local, "remotePort": remote,
      ])
      let _: Acknowledgment = try await client.request("POST", "/v1/forwards", body: data)
      section = .forwards
      await refreshResources()
      showToast("Port forward created")
    } catch { self.error = error.localizedDescription }
  }
  func removeForward(_ id: String) async {
    do {
      let _: Acknowledgment = try await client.request("DELETE", "/v1/forwards/\(id)")
      await refreshResources()
    } catch { self.error = error.localizedDescription }
  }
  func showDiagnostics() async {
    do {
      diagnostics = try await client.request("GET", "/v1/diagnostics", as: TextResponse.self).text
    } catch { self.error = error.localizedDescription }
  }
  func showToast(_ text: String) {
    toast = text
    Task {
      try? await Task.sleep(for: .seconds(4))
      if toast == text { toast = nil }
    }
  }
  func notify(_ title: String, _ body: String) {
    guard UserDefaults.standard.bool(forKey: "notifications") else { return }
    let content = UNMutableNotificationContent()
    content.title = title
    content.body = body
    UNUserNotificationCenter.current().add(
      UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
  }
}
