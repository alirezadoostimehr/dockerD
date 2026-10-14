import SwiftUI

struct ResourcesView: View {
  @EnvironmentObject private var model: AppModel
  @State private var remove: Resource?
  @State private var inputAction: String?
  @State private var input = ""
  @State private var forwardPod: Resource?
  @State private var runImage: Resource?
  var body: some View {
    VStack(spacing: 0) {
      if model.status?.running != true {
        EmptyPanel(
          symbol: model.section.symbol, title: "Your engine is resting",
          description:
            "Start the engine to manage \(model.section == .pods ? "Kubernetes pods" : model.section.key). Your existing data is preserved."
        ) { EmptyView() }
      } else if model.section == .pods && model.status?.settings.kubernetes == false {
        EmptyPanel(
          symbol: "cube.transparent", title: "Kubernetes is disabled",
          description: "Stop the engine, enable Kubernetes in Settings, and start it again."
        ) { Button("Open settings") { model.section = .settings } }
      } else {
        toolbar
        if model.filtered.isEmpty {
          EmptyPanel(
            symbol: model.section.symbol,
            title: model.search.isEmpty
              ? "No \(model.section == .pods ? "pods" : model.section.key) yet" : "No matches",
            description: emptyDescription
          ) {
            if model.section == .images {
              Button("Pull an image") { inputAction = "pull" }.buttonStyle(.borderedProminent)
            }
          }
        } else {
          ScrollView {
            LazyVStack(spacing: 1) {
              ForEach(groups, id: \.self) { group in
                if !group.isEmpty {
                  HStack {
                    Image(systemName: model.section == .pods ? "folder" : "square.stack.3d.up")
                    Text(group)
                    Spacer()
                  }.font(.system(size: 11, weight: .semibold)).foregroundStyle(.secondary).padding(
                    .horizontal, 16
                  ).padding(.top, 18).padding(.bottom, 8)
                }
                ForEach(model.filtered.filter { $0.group == group }, id: \.identity) { resource in
                  row(resource)
                }
              }
            }.padding(.horizontal, 30).padding(.bottom, 16)
          }
        }
        if let selected = model.selected {
          detailPanel(selected)
            .task(id: selected.identity + model.detailView) { await model.loadDetail(selected) }
        }
      }
    }
    .confirmationDialog(
      "Remove \(remove?.name ?? "resource")?",
      isPresented: Binding(get: { remove != nil }, set: { if !$0 { remove = nil } })
    ) {
      Button("Remove", role: .destructive) {
        if let resource = remove { Task { await model.action("remove", resource: resource) } }
        remove = nil
      }
    } message: {
      Text(
        model.section == .volumes
          ? "Data in this volume will be permanently deleted. Volumes in use cannot be removed."
          : "This operation cannot be undone. Resources still in use may need to be stopped or detached first."
      )
    }
    .sheet(
      isPresented: Binding(
        get: { inputAction != nil },
        set: {
          if !$0 {
            inputAction = nil
            input = ""
          }
        })
    ) {
      VStack(alignment: .leading, spacing: 20) {
        Text(
          inputAction == "pull"
            ? "Pull an image" : inputAction == "tag" ? "Tag image" : "Create a volume"
        ).font(.title2.bold())
        Text(
          inputAction == "pull"
            ? "Use an image name and tag, such as nginx:alpine."
            : inputAction == "tag"
              ? "Enter the new repository and tag." : "Choose a name for persistent storage."
        ).foregroundStyle(.secondary)
        TextField(inputAction == "createVolume" ? "my-volume" : "nginx:alpine", text: $input)
          .textFieldStyle(.roundedBorder).onSubmit { submitInput() }
        HStack {
          Spacer()
          Button("Cancel") {
            inputAction = nil
            input = ""
          }
          Button("Continue") { submitInput() }.buttonStyle(.borderedProminent).disabled(
            input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
      }.padding(28).frame(width: 450)
    }
    .sheet(item: $forwardPod) { pod in ForwardForm(pod: pod).environmentObject(model) }
    .sheet(item: $runImage) { image in RunImageForm(image: image).environmentObject(model) }
  }
  var groups: [String] { Set(model.filtered.map(\.group)).sorted() }
  var emptyDescription: String {
    if !model.search.isEmpty { return "Try another name, image, or project." }
    switch model.section {
    case .containers:
      return
        "Run a container from Terminal with docker --context dockerd-local run, or use Docker Compose."
    case .images: return "Pull your first image to begin building locally."
    case .volumes:
      return "Create a volume here or let Docker Compose create one for your application."
    default: return "Deploy a workload with kubectl using the DockerD kubeconfig shown in Settings."
    }
  }
  var toolbar: some View {
    HStack(spacing: 12) {
      HStack {
        Image(systemName: "magnifyingglass").foregroundStyle(.tertiary)
        TextField(
          "Search \(model.section == .pods ? "pods" : model.section.key)…", text: $model.search
        ).textFieldStyle(.plain)
      }.padding(10).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 8))
        .frame(maxWidth: 360)
      if model.section == .pods {
        Picker("Namespace", selection: $model.namespace) {
          ForEach(model.namespaces, id: \.self) { Text($0) }
        }.labelsHidden().frame(maxWidth: 200)
      }
      Spacer()
      Text("\(model.filtered.count) items").font(.caption).foregroundStyle(.tertiary)
      Button {
        Task { await model.refreshResources() }
      } label: {
        Image(systemName: "arrow.clockwise")
      }.help("Refresh resources")
      if model.section == .images {
        Button {
          inputAction = "pull"
        } label: {
          Label("Pull image", systemImage: "arrow.down")
        }.buttonStyle(.borderedProminent)
      }
      if model.section == .volumes {
        Button {
          inputAction = "createVolume"
        } label: {
          Label("Create volume", systemImage: "plus")
        }.buttonStyle(.borderedProminent)
      }
    }.padding(.horizontal, 30).padding(.bottom, 18)
  }
  func row(_ resource: Resource) -> some View {
    HStack(spacing: 14) {
      Image(systemName: model.section.symbol).font(.system(size: 18)).foregroundStyle(
        accent.opacity(0.8)
      ).frame(width: 38, height: 38).background(
        accent.opacity(0.06), in: RoundedRectangle(cornerRadius: 8))
      VStack(alignment: .leading, spacing: 5) {
        Text(resource.name).font(.system(size: 12, weight: .semibold)).lineLimit(1)
        Text(resource.subtitle).font(.system(size: 10, design: .monospaced)).foregroundStyle(
          .secondary
        ).lineLimit(1)
      }.frame(maxWidth: .infinity, alignment: .leading)
      if !resource.ports.isEmpty {
        Text(resource.ports).font(.system(size: 10, design: .monospaced)).foregroundStyle(
          .secondary
        ).lineLimit(2).frame(maxWidth: 130)
      }
      if resource.size > 0 {
        Text(ByteCountFormatter.string(fromByteCount: resource.size, countStyle: .binary)).font(
          .caption
        ).foregroundStyle(.secondary)
      }
      StatusBadge(state: resource.state).frame(width: 92, alignment: .leading)
      Menu {
        Button("Inspect") { select(resource, view: "inspect") }
        if model.section == .containers || model.section == .pods {
          Button("View logs") { select(resource, view: "logs") }
          Button("Open terminal") { Task { await model.action("terminal", resource: resource) } }
            .disabled(!resource.active)
        }
        if model.section == .containers {
          Button(resource.active ? "Stop" : "Start") {
            Task { await model.action(resource.active ? "stop" : "start", resource: resource) }
          }
          Button("Restart") { Task { await model.action("restart", resource: resource) } }
        }
        if model.section == .pods { Button("Forward a port…") { forwardPod = resource } }
        if model.section == .images {
          Button("Run…") { runImage = resource }
          Button("Tag…") {
            model.selected = resource
            inputAction = "tag"
          }
        }
        Divider()
        Button("Remove…", role: .destructive) { remove = resource }
      } label: {
        Image(systemName: "ellipsis").frame(width: 22)
      }.menuStyle(.borderlessButton).frame(width: 30)
    }.padding(14).background(
      model.selected?.identity == resource.identity
        ? accent.opacity(0.07) : Color(nsColor: .controlBackgroundColor),
      in: RoundedRectangle(cornerRadius: 10)
    ).overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(.primary.opacity(0.04)))
      .contentShape(Rectangle()).onTapGesture { select(resource, view: "inspect") }
      .accessibilityElement(children: .contain)
  }
  func select(_ resource: Resource, view: String) {
    model.detail = "Loading…"
    model.detailView = view
    model.selected = resource
  }
  func detailPanel(_ selected: Resource) -> some View {
    VStack(spacing: 0) {
      Divider()
      HStack {
        Text(selected.name).font(.system(size: 12, weight: .semibold)).lineLimit(1)
        Spacer()
        Picker("View", selection: $model.detailView) {
          Text("Inspect").tag("inspect")
          if model.section == .containers || model.section == .pods { Text("Logs").tag("logs") }
        }.pickerStyle(.segmented).frame(width: 170)
        Button {
          Task { await model.loadDetail(selected) }
        } label: {
          Image(systemName: "arrow.clockwise")
        }
        Button {
          model.selected = nil
        } label: {
          Image(systemName: "xmark")
        }
      }.padding(.horizontal, 30).padding(.vertical, 12)
      ScrollView([.horizontal, .vertical]) {
        Text(model.detail).font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
          .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 30).padding(
            .bottom, 16)
      }.frame(height: 200)
    }.background(Color(nsColor: .textBackgroundColor))
  }
  func submitInput() {
    guard let action = inputAction else { return }
    let value = input.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty else { return }
    let selected = model.selected
    inputAction = nil
    input = ""
    Task { await model.action(action, resource: selected, value: value) }
  }
}

struct ForwardForm: View {
  @EnvironmentObject private var model: AppModel
  @Environment(\.dismiss) private var dismiss
  var pod: Resource?
  @State private var podName = ""
  @State private var namespace = "default"
  @State private var local = 8080
  @State private var remote = 80
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      Text("Forward a pod port").font(.title2.bold())
      Text(
        "Available only on localhost. An open forward keeps the engine awake, even without traffic."
      ).font(.callout).foregroundStyle(.secondary)
      Form {
        TextField("Pod", text: $podName)
        TextField("Namespace", text: $namespace)
        TextField("Local port", value: $local, format: .number.grouping(.never))
        TextField("Pod port", value: $remote, format: .number.grouping(.never))
      }
      HStack {
        Spacer()
        Button("Cancel") { dismiss() }
        Button("Start forwarding") {
          Task {
            await model.createForward(
              pod: podName, namespace: namespace, local: local, remote: remote)
          }
          dismiss()
        }.buttonStyle(.borderedProminent).disabled(
          podName.isEmpty || namespace.isEmpty || !(1024...65535).contains(local)
            || !(1...65535).contains(remote))
      }
    }.padding(28).frame(width: 470).onAppear {
      if let pod {
        podName = pod.name
        namespace = pod.namespace
      }
    }
  }
}
struct ForwardsView: View {
  @EnvironmentObject private var model: AppModel
  @State private var showingForm = false
  var body: some View {
    VStack(spacing: 16) {
      HStack {
        Text("Forwarded ports remain active when this window closes.").font(.caption)
          .foregroundStyle(.secondary)
        Spacer()
        Button {
          showingForm = true
        } label: {
          Label("New forward", systemImage: "plus")
        }.buttonStyle(.borderedProminent).disabled(
          model.status?.running != true || model.status?.settings.kubernetes != true)
      }.padding(.horizontal, 30)
      if model.forwards.isEmpty {
        EmptyPanel(
          symbol: "arrow.left.arrow.right", title: "No open port forwards",
          description:
            "Connect a local port to a Kubernetes pod to access your application from this Mac."
        ) { EmptyView() }
      } else {
        ScrollView {
          VStack(spacing: 12) {
            ForEach(model.forwards) { forward in
              Surface {
                HStack {
                  Image(systemName: "arrow.left.arrow.right").foregroundStyle(accent)
                  VStack(alignment: .leading, spacing: 5) {
                    Text("localhost:\(String(forward.localPort)) → \(String(forward.remotePort))")
                      .font(.system(size: 14, weight: .semibold, design: .monospaced))
                    Text("\(forward.namespace)/\(forward.pod)").font(.caption).foregroundStyle(
                      .secondary)
                    if let error = forward.error {
                      Text(error).font(.caption).foregroundStyle(.red)
                    }
                  }
                  Spacer()
                  StatusBadge(state: forward.state)
                  Button("Stop", role: .destructive) {
                    Task { await model.removeForward(forward.id) }
                  }
                }
              }
            }
          }.padding(.horizontal, 30).padding(.bottom, 30)
        }
      }
    }.sheet(isPresented: $showingForm) { ForwardForm().environmentObject(model) }
  }
}
