# mcp-docs in a container

`Containerfile` builds an image that serves MCP over Streamable HTTP on
port 8080: one JSON-RPC message per POST to `/mcp`, health at
`/healthz` (docs/architecture.md, §13). The server is stateless, so any
number of containers can serve one endpoint behind a load balancer.

```sh
podman build -f packaging/container/Containerfile --build-arg VERSION=0.1.0 -t mcp-docs .
podman run --rm -p 8080:8080 --read-only mcp-docs
claude mcp add --transport http docs http://localhost:8080/mcp
```

| | |
|---|---|
| Base | `registry.suse.com/bci/bci-micro:16.0` (`--build-arg BASE=scratch` works too: the program is static) |
| Builder | `registry.suse.com/bci/golang:1.24` (`--build-arg BUILDER=…`) |
| Program | `/usr/bin/mcp-docs --listen :8080` (the entrypoint; arguments after the image name are added) |
| User | 65532; nothing is written, so `--read-only` works |
| Collections | `/usr/share/mcp-docs/collections.d`; the image has one, `mcp-docs`, its own documentation |
| Network | listens on 8080 and connects nowhere (Landlock, where the kernel and the runtime's seccomp profile allow it: the log's first line says) |

## An image for your documentation

Build on the image: add the documents and their collection file, and
disable the demo collection with an empty file of its name in `/etc`
(`empty`: an empty file in the build context).

```dockerfile
FROM mcp-docs:0.1.0
COPY sles16/ /usr/share/mcp-docs/docs/sles16/
COPY sles16.yaml /usr/share/mcp-docs/collections.d/sles16.yaml
COPY empty /etc/mcp-docs/collections.d/mcp-docs.yaml
```

Do not add `--chmod` to a `COPY` that creates directories: BuildKit
gives the new parent directories the same mode, and a directory of mode
0644 cannot be entered by the server's user (the demo collection then
stays, and the log says `permission denied`). Files readable by all and
directories of mode 0755, as a build context usually has them, are what
the server needs.

```yaml
# sles16.yaml
title: SUSE Linux Enterprise Server 16.0
root: /usr/share/mcp-docs/docs/sles16
description: The SLES 16.0 documentation, converted from SUSE's sources.
```

Tag the image with the documentation's version: updating the
documentation is a rebuild, and nothing in a running container
downloads or converts anything.

## Behind an ingress

The server does no TLS and no authentication: put it behind an ingress
or reverse proxy that does (TLS, then mTLS, OAuth or tokens as the
deployment needs). Health checks: `GET /healthz`. A client in a
browser needs its origin allowed: `--allow-origin https://app.example`
(after the image name, as an argument).

Kubernetes probes, for example:

```yaml
readinessProbe:
  httpGet: {path: /healthz, port: 8080}
livenessProbe:
  httpGet: {path: /healthz, port: 8080}
securityContext:
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  allowPrivilegeEscalation: false
```
