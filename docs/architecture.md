# Architecture and shutdown contract

DockerD consists of a SwiftUI application, a local Go control service, and one Lima-managed Linux VM using Apple's Virtualization framework. Docker Engine and K3s share the Docker runtime. The app is an independent product, with its own branding and core Docker Desktop-style workflows.

The host service exposes versioned JSON endpoints through a private Unix socket. Docker and Kubernetes contexts point at monitored local gateways. Persistent state belongs under the user's Application Support directory. VM state is isolated with a private LIMA_HOME.

The Docker gateway accepts HTTP/1.1, native cleartext HTTP/2, and legacy BuildKit HTTP/2 upgrades. Build RPCs are tracked independently from read-only history/status watches; merely keeping a BuildKit connection open does not imply a running build. Kubernetes upgraded streams remain tracked through their full connection lifetime, with audit records providing an independent session reconciliation source.

## Idle contract

The default is 10 continuous idle minutes followed by a 60-second cancelable countdown. Running or paused user containers, nonterminal user pods, exec/attach sessions, port forwards, builds, transfers, and runtime mutations block automatic shutdown. Only specifically identified bundled system infrastructure is exempt. An open session counts even when no traffic flows.

Unknown or stale activity is a blocker. Shutdown requires a fresh observation, an admission gate against new work, and a final activity check. Sleep, monitor gaps, and service restarts reset the idle baseline. Shutdown is graceful and preserves disks. Wake is explicit.

The policy is clock-driven and tested separately from the VM. The control service serializes lifecycle operations, uses request-generation counters to catch activity between polls, and re-evaluates the deadline after gating so a concurrent Keep running action wins over shutdown. Audit corruption, rotation, or missing lifecycle records fail closed; partial records remain unknown until completed.

Runtime artifacts are checksum-locked. Linux image downloads use resumable range requests and are published only after full verification. Build scripts prepare runtime binaries and app bundles in staging directories before replacing their destinations, avoiding in-place modification of running Mach-O executables.

## Delivery boundaries

The initial release targets Apple Silicon and macOS 14+. It provides containers, images, volumes, Kubernetes pods, logs, terminals, port forwards, settings, and menu-bar controls. Full Docker Desktop feature parity, extensions, Intel builds, cloud integrations, and automatic migration are outside this release.

## References

- https://lima-vm.io/docs/config/vmtype/vz/
- https://lima-vm.io/docs/config/port/
- https://docs.k3s.io/advanced
- https://docs.docker.com/reference/api/engine/
- https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/
- https://docs.docker.com/engine/release-notes/29/
