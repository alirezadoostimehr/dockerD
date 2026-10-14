# Operating DockerD

## State and endpoints

Private application state lives in `~/Library/Application Support/DockerD/`:

| Path or endpoint | Purpose |
| --- | --- |
| `control.sock` | Versioned local JSON control API; no TCP listener |
| `docker.sock` | Monitored Docker API for `dockerd-local` |
| `settings.json` | Engine and idle settings |
| `kubeconfig` | User context through the monitored Kubernetes gateway |
| `kubeconfig-internal` | Internal authenticated monitoring connection |
| `infrastructure.json` | Trusted Kubernetes controller identities |
| `lima/dockerd/` | VM disk, SSH state, and Lima configuration |
| `images/linux.img` | Verified first-boot image cache |
| `runtime.log`, `service.log` | Startup and service diagnostics |
| `127.0.0.1:16443` | Internal Kubernetes endpoint forwarded from the VM |
| `127.0.0.1:16444` | Monitored gateway; TLS client certificate required |

Kubeconfigs and VM state contain local administrative credentials. Use the monitored Docker socket and public kubeconfig for ordinary work. Direct `limactl shell` is intended for diagnostics; arbitrary guest processes outside Docker/Kubernetes are not idle-policy inputs.

## Resources and sessions

Stop the engine before changing CPU, RAM, shared folders, or Kubernetes. Defaults: up to four host cores, four GiB RAM capped at half host memory, and a 60 GiB sparse disk. Disk capacity is fixed after creating the VM. No destructive reset or migration runs automatically.

Only selected folders are mounted read-write. Published container ports and pod forwards are local to this Mac. Forwards created in DockerD remain active after the window closes; stop them from Port forwards. Interactive terminals open in macOS Terminal and stay active for their actual session lifetime.

## Troubleshooting

- **Download interrupted:** retry Start. The Linux image resumes from a private partial file and is used only after its SHA-256 check passes. Provisioning requires access to Ubuntu, GitHub, Docker downloads, and image registries.
- **Startup fails:** open Overview → View diagnostics. VM data remains intact for retry. Resolve port collisions and missing shared folders first.
- **Ports 16443/16444 are occupied:** stop the conflicting service or other DockerD test instance. This release supports one Kubernetes VM per Mac.
- **CLI reports engine stopped:** explicitly Start engine. Polling tools do not wake the VM.
- **Automatic shutdown never occurs:** inspect Overview's blockers. Running servers, open terminals, and unused but open forwards count. Monitoring failures block shutdown intentionally. Delayed Kubernetes cleanup can reset the idle baseline after a workload ends.
- **Audit rotated or a session cannot be reconciled:** stop and restart the engine. The app establishes a fresh audit baseline instead of guessing that a lost session ended.
- **Service interrupted:** reopen DockerD. It reacquires a process lock, reconnects to a surviving VM, reloads infrastructure identities, and reconciles activity before counting idle time.
- **CLI links exist:** the installer refuses to replace them. Keep existing tools or manage those links explicitly. The UI uses its bundled binaries independently.
- **App moved after CLI installation:** links reference the bundle. Update/remove only the old DockerD links, then install again from Settings.

Quit → Leave engine running intentionally leaves the small service active. There is no login LaunchAgent; optional macOS login registration starts the app, which starts or reconnects to the service.

## Private control API

The SwiftUI app uses HTTP/1.1 over `control.sock`. Responses are JSON and errors use `{ "error": "message" }`. Routes under `/v1` include:

- `GET /status`: lifecycle, settings, monitor health, blockers, idle deadline, VM metrics.
- `POST /start`, `/stop`, `/idle/reset`: lifecycle and countdown controls.
- `PUT /settings`: validated settings; runtime changes require a stopped VM.
- `GET /resources/{containers|images|volumes|pods}`, `/detail`: inventory, logs, inspection.
- `POST /action`: resource operations and terminal launch descriptors.
- `GET|POST /forwards`, `DELETE /forwards/{id}`: managed pod forwards.
- `POST /context/activate`, `/cli/install`: explicit CLI integration.
- `GET /diagnostics`: recent runtime log output.

The service serializes VM lifecycle and observation, gates new mutations before shutdown, and rechecks both activity and the current deadline immediately before stopping.

## Distribution signing

`make app` uses an ad-hoc signature. To build and notarize with your own identity:

```sh
DOCKERD_SIGN_IDENTITY='Developer ID Application: Your Name (TEAMID)' make app
DOCKERD_NOTARY_PROFILE='your-existing-keychain-profile' sh scripts/notarize.sh
```

Nested macOS executables are signed before the outer bundle; Lima receives its virtualization entitlement. Signing credentials come from your existing keychain configuration and are not stored here. Notarization requires a valid Apple account profile and is never run automatically by `make app`.
