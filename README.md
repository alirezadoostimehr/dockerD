# DockerD

A native macOS desktop for a local Docker Engine and Kubernetes cluster, with explainable, configurable idle shutdown. Apple Silicon, macOS 14 or newer. Docker Desktop is not required.

## Development

Requires Xcode Command Line Tools, Swift 5.10+, Go 1.23+, Python 3, and internet access for runtime downloads.

```sh
make test
make app
open dist/DockerD.app
```

The app uses its own Lima VM and `dockerd-local` contexts. Existing Docker and Kubernetes environments are not migrated or deleted. Runtime binaries, VM disks, credentials, logs, and downloaded assets are never committed.

See [architecture](docs/architecture.md) for the runtime and shutdown contract. Build and operating instructions will be expanded alongside implementation.
