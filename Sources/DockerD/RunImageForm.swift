import SwiftUI

struct RunImageForm: View {
  @EnvironmentObject private var model: AppModel
  @Environment(\.dismiss) private var dismiss
  let image: Resource
  @State private var name = ""
  @State private var publish = false
  @State private var local = 8080
  @State private var container = 80
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      Text("Run a container").font(.title2.bold())
      Text(image.name).font(.system(.callout, design: .monospaced)).foregroundStyle(.secondary)
      Form {
        TextField("Name (optional)", text: $name)
        Toggle("Publish a port on localhost", isOn: $publish)
        if publish {
          TextField("Local port", value: $local, format: .number.grouping(.never))
          TextField("Container port", value: $container, format: .number.grouping(.never))
        }
      }
      Text(
        "For environment variables, bind mounts, and multi-container projects, use Docker Compose or the CLI."
      ).font(.caption).foregroundStyle(.secondary)
      HStack {
        Spacer()
        Button("Cancel") { dismiss() }
        Button("Run") {
          Task {
            await model.runImage(
              image, name: name, local: publish ? local : 0, container: publish ? container : 0)
          }
          dismiss()
        }.buttonStyle(.borderedProminent).disabled(
          publish && (!(1024...65535).contains(local) || !(1...65535).contains(container)))
      }
    }.padding(28).frame(width: 460)
  }
}
