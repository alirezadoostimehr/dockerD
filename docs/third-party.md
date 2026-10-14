# Third-party components

DockerD is an independent desktop application. It is not Docker Desktop and is not affiliated with Docker, Inc.

The runtime versions and artifact SHA-256 digests are recorded in `resources/runtime-lock.json`.

| Component | License | Source |
| --- | --- | --- |
| Lima | Apache-2.0 | https://github.com/lima-vm/lima |
| Docker CLI / Moby Engine | Apache-2.0 | https://github.com/docker/cli / https://github.com/moby/moby |
| Docker Compose | Apache-2.0 | https://github.com/docker/compose |
| Docker Buildx | Apache-2.0 | https://github.com/docker/buildx |
| Kubernetes / kubectl | Apache-2.0 | https://github.com/kubernetes/kubernetes |
| K3s | Apache-2.0 | https://github.com/k3s-io/k3s |

The Apache license text is included in `resources/licenses/Apache-2.0.txt` and the packaged app. Lima's own bundled license is retained. These binaries incorporate additional dependencies under their respective upstream licenses; consult each pinned upstream release's source and dependency notices for redistribution.

The Ubuntu 24.04 guest image is downloaded on first launch. Its packages have individual licenses, documented under `/usr/share/doc/*/copyright` inside the VM. See https://ubuntu.com/legal/intellectual-property-policy.

DockerD uses macOS system frameworks supplied by Apple. No Docker Desktop application code, branding assets, or proprietary extensions are bundled.
