# Tailgate Controller

> ⚠️ Ingress is broken. You just didn’t realize it yet.

### Why?

If you're running a homelab and want to share access to something, you’ve basically got two options:

1. Get a public IP, deal with port forwarding, dynamic DNS, your ISP blocking stuff, or paying extra for a static IP.
2. Use Tailscale and share access to one of your nodes.

The problem? Sharing a Tailscale node shares *everything* on that machine. Every open port, every service. Yeah, you can do ACLs and routing... *if* you’re on the Pro plan. But most of us are not paying \$5/user/month to share Plex with a friend.

**That’s where Tailgate comes in.**

This Kubernetes controller gives you a Tailscale node *per-service*, so you can expose *just* the thing you want — and nothing else. Creepy Karthik can watch your Plex, but he’s not getting anywhere near your NAS, dev cluster, or dashboard.

---

## Table of Contents

* [Why Tailgate?](#why-tailgate)
* [Use Cases](#use-cases)

  * [Plex for Creepy Karthik](#1-plex-for-creepy-karthik)
  * [Photo Server for Family](#2-photo-server-for-family)
  * [Dev Access for Contractors](#3-dev-access-for-contractors)
* [Features](#features)
* [Prerequisites](#prerequisites)
* [Installation](#installation)
* [Configuration](#configuration)

  * [TSIngress Spec](#tsingress-spec)
  * [Proxy Rules](#proxy-rules)
* [Security Considerations](#security-considerations)
* [Troubleshooting](#troubleshooting)
* [Contributing](#contributing)
* [License](#license)

---

## Why Tailgate?

Because typical ingress sucks when it comes to security:

* **Traditional ingress controllers** route based on headers (like Host), which are trivial to spoof.
* **Tailscale node sharing** gives access to everything, unless you go full Pro and configure ACLs properly.
* **Public IPs** are annoying or expensive, and many ISPs block ports or double-NAT you anyway.

Tailgate fixes all of this:

* Gives you a *dedicated* Tailscale node per-service
* Only exposes the service you specify — no port scanning, no lateral movement
* Works with the **free** Tailscale plan
* Supports **Funnel** for public access — even if your ISP sucks
* No need for complicated ACLs or manual Tailscale node juggling
* Use well-known Funnel ports (443, 8443, or 10000) — works even for Grandma

Also unlike using Funnel directly, Tailgate lets you expose *any* internal port using those limited public ones. So if you've got Redis running on port 6379, you can easily forward it through 443, and on the client just use port 443 — it Just Works™.

---

## Use Cases

### 1. Plex for Creepy Karthik

Karthik wants to watch movies on your Plex. You’re cool with that. What you’re *not* cool with is him trying to poke around your NAS or Home Assistant setup.

```yaml
apiVersion: tailscale.tailgate.run/v1alpha1
kind: TSIngress
metadata:
  name: plex-for-karthik
  namespace: media
spec:
  backendService: plex
  proxyRules:
    - protocol: tcp
      listenPort: 32400
      backendPort: 32400
      funnel: false
  hostnames:
    - plex-karthik
  tailnetName: your-tailnet.ts.net
```

Now he only sees Plex. Even if he gets your login creds, he can’t do anything else — because the *node he sees doesn’t have access to anything else*. Zero-trust, literally.

### 2. Photo Server for Family

You want to share Immich with your parents. They aren’t installing Tailscale or dealing with VPNs. You just want to send them a link that works.

```yaml
apiVersion: tailscale.tailgate.run/v1alpha1
kind: TSIngress
metadata:
  name: immich-parents
  namespace: photos
spec:
  backendService: immich
  proxyRules:
    - protocol: tcp
      listenPort: 443
      backendPort: 3001
      funnel: true
  hostnames:
    - family-photos
  tailnetName: your-tailnet.ts.net
```

With **Funnel** enabled and using one of the public ports (443/8443/10000), they can access your service via a simple link. No VPN, no app install, no tech support required.

### 3. Dev Access for Contractors

You’ve got external folks who need to hit `dev-api`. You *don’t* want them anywhere near staging, production, or other internal tools.

```yaml
apiVersion: tailscale.tailgate.run/v1alpha1
kind: TSIngress
metadata:
  name: dev-access
  namespace: development
spec:
  backendService: dev-api
  proxyRules:
    - protocol: tcp
      listenPort: 443
      backendPort: 8080
      funnel: false
  hostnames:
    - dev-api
  tailnetName: your-tailnet.ts.net
```

Give them access to that Tailscale node only. Done. They can’t even *see* your other services.

---

## Features

* ✅ One Tailscale node per service
* ✅ Share specific services securely
* ✅ Optional Funnel support for public URLs
* ✅ Works without Tailscale Pro
* ✅ Any port forwarding (not just 443/3306)
* ✅ Built for K8s homelabbers

---

## Prerequisites

* Kubernetes cluster
* Tailscale account (free works!)
* `kubectl` configured
* Docker (for local builds, optional)
* Go (for dev work)

---

## Installation

```sh
helm install tailgate-controller tailgate/tailgate-controller \
  --namespace tailgate-controller-system \
  --create-namespace \
  --set auth.tailscale.data.TAILSCALE_CLIENT_ID=your-client-id \
  --set auth.tailscale.data.TAILSCALE_CLIENT_SECRET=your-client-secret
```

---

## Configuration

### TSIngress Spec

| Field            | Type    | Description                           |
| ---------------- | ------- | ------------------------------------- |
| `backendService` | string  | Kubernetes service to expose          |
| `proxyRules`     | array   | See below                             |
| `hostname`      | array   | Hostname for the Tailscale node (array of strings was a mistake, only pass one string please)     |
| `tailnetName`    | string  | Your tailnet (e.g. `yourname.ts.net`) |
| `ephemeral`      | boolean | Use ephemeral auth keys (optional)    |
| `updateDNS`      | boolean | Automatically manage DNS (optional)   |
| `domain`         | string  | DNS domain (if `updateDNS` is true)   |
| `dnsName`        | string  | DNS prefix (optional)                 |

### Proxy Rules

| Field         | Type   | Description                             |
| ------------- | ------ | --------------------------------------- |
| `protocol`    | string | `tcp` ( `udp` is still a work in progress)                         |
| `listenPort`  | int    | Port the Tailscale node listens on      |
| `backendPort` | int    | Port inside your cluster it forwards to |
| `funnel`      | bool   | Should this rule go through Funnel?     |

---

## Security Considerations

* Every TSIngress is a new, isolated Tailscale node
* No lateral movement across services
* Works great even if someone steals your Tailscale login
* Funnel gives temporary, limited public access
* No TLS or header spoofing attacks possible
* Public ports (443, 8443, 10000) allow you to serve anything from anywhere

---

## Troubleshooting

**Service not accessible?**

* Check the `TSIngress` status
* Make sure your backend service is up
* Confirm Tailscale auth worked (check logs)

**Funnel not working?**

* You need to enable Funnel in the Tailscale dashboard
* Funnel supports public access on ports 443, 8443, and 10000
* Tailgate lets you forward *any* internal port through one of those externally, even if your app isn't normally web-friendly

---

## Contributing

PRs welcome! This is still early days, so expect rough edges. If it’s broken or missing something, open an issue or fix it yourself and send a PR.

---

## License

Apache 2.0. Use it, fork it, do what you want — just don’t sell it as-is without giving credit.
