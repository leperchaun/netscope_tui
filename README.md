# netscope

A small terminal network monitor written in Go. It shows live interface throughput, TCP connection states, and TCP-connect latency to targets you choose. Inspired by [netwatch](https://github.com/matthart1983/netwatch) as a design reference; the code is independent.

## What it shows

- **Throughput**: download and upload rates with sparklines, summed across interfaces
- **Interfaces**: per-interface rates and totals (loopback excluded)
- **Latency**: TCP connect time to each `host:port` target
- **TCP states**: count of sockets by state (ESTABLISHED, TIME_WAIT, ...)

## Run

Native binary (macOS, Windows, Linux):

```bash
make build
./bin/netscope                          # live TUI, q to quit
./bin/netscope -targets example.com:443,10.0.0.1:22 -interval 1s
./bin/netscope -once                    # one JSON snapshot, for scripts
```

Container (Linux, where host networking works):

```bash
docker compose run --rm netscope        # uses network_mode: host, pid: host
```

Inside the container, `auto` finds Docker Desktop's internal resolver (`192.168.65.x`), not your network's. Pass your real resolver instead:

```bash
NETSCOPE_DNS=192.168.2.88 docker compose run --rm netscope
```

Use `run`, not `up`: `up` multiplexes container output with a `netscope-1 |` prefix, which garbles the TUI.

## Platform notes

Go builds a native binary for each OS, so the native binary is the reliable path on all three platforms.

| Platform | Native binary | Container |
|---|---|---|
| Linux | Full view of host | Full view of host with `network_mode: host` and `pid: host` |
| macOS | Full view of host | Docker Desktop runs a Linux VM. The container sees the VM's network, not your Mac's. |
| Windows | Full view of host | Same as macOS, the container sees the WSL/Hyper-V VM's network. |

So the container is mainly a good fit for monitoring a Linux host or VM. On Mac and Windows, use the native binary to see your own machine.

Cross-compile all platforms into `dist/`:

```bash
make release
```

Note that Apple's `/usr/bin/make` requires an accepted Xcode license (`sudo xcodebuild -license`). Plain `go build` works without it since the build sets `CGO_ENABLED=0`.

## Not yet implemented

- Packet capture and protocol decoding (would need libpcap/Npcap and elevated privileges)
- Process attribution for connections
- Default-gateway detection (targets are passed explicitly for now)
- Alerts or history persistence
