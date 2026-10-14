# Validation record

Validated on Apple Silicon, macOS 27.0.1, using Swift 6.4 and Go 1.27.1 on 2026-10-28. The app deployment target is macOS 14; an older macOS host was not available for this run.

## Automated checks

- `make test`: Go tests with the race detector, `go vet`, and Swift tests passed.
- Idle-policy cases cover active work, countdown timing, cancellation, stale/unknown observations, disabled settings, host sleep gaps, and clock changes.
- Gateway tests cover admission races, streaming connections, gRPC disconnects, and tracking BuildKit RPCs separately from idle history watches over both native HTTP/2 and legacy upgrades.
- Audit tests cover open/closed sessions, partial records across polls, corruption, rotation, and explicit recovery.
- Download tests verify resuming a partial image and rejecting a checksum mismatch.
- Settings/API tests verify private settings storage, invalid configurations, stopped-engine action rejection, and restrictions on live runtime changes.
- Swift tests verify chunked local HTTP responses, truncated responses, and service error propagation.

## Live VM checks

The complete `scripts/integration-test.py` suite passed against the dedicated Lima test VM:

1. Start Docker Engine and K3s.
2. Run a container through the app API and serve HTTP through its published port.
3. Deploy a Kubernetes pod and serve HTTP through an app-managed port forward.
4. Recognize external kubectl exec and pod port-forward sessions as shutdown blockers.
5. Return an empty cluster to healthy idle monitoring.
6. Enter the idle countdown and cancel it with Keep running.
7. Automatically shut down the VM after the next idle interval.
8. Reject CLI requests while stopped without waking the VM.
9. Restart explicitly and read a previously written sentinel from a persistent volume.

Additional checks passed: Docker Buildx built and loaded a real image through the monitored gateway; Docker Compose started an nginx service, served HTTP 200, and removed its resources. The service also recovered an already-running VM after an interrupted development session.

## Packaging

`make app` built `dist/DockerD.app`, bundled the locked tools and license notices, and passed `codesign --verify --deep --strict`. The service and Lima binaries inside that signed bundle successfully started the test VM with healthy Docker/Kubernetes monitoring. The local build uses an ad-hoc signature. Developer ID notarization requires the user's credentials and was not performed.

The packaged app was opened and its actual Overview window was visually inspected; a capture is included in `docs/images/overview.png`. Full manual UI accessibility testing and older-macOS compatibility testing remain release-validation tasks. The native views use standard macOS controls, labels, keyboard commands, and appearance settings.
