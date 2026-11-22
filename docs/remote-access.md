# Remote access: dwnvr on your phone, at home and away <!-- omit in toc -->

This guide takes dwnvr from `http://<machine-ip>:8080` on the home network to
an **`https://`** address that opens from anywhere, **with the browser offering
to install the page as an app**. It also shows how to give other people, such
as your family, access **without exposing the server to the internet**.

The way there is [Tailscale](https://tailscale.com): a VPN that connects your
devices to the server without opening a port on the router, and that issues
the `https://` certificate. There is nothing to configure in dwnvr.

> For `https://` only inside the home, without Tailscale, see [https-local-network.md](https-local-network.md).

- [Why HTTPS](#why-https)
- [Why not expose the server to the internet](#why-not-expose-the-server-to-the-internet)
- [The server: Tailscale and `tailscale serve`](#the-server-tailscale-and-tailscale-serve)
  - [1. Tailscale on the server](#1-tailscale-on-the-server)
  - [2. The `serve`](#2-the-serve)
  - [3. The cost](#3-the-cost)
- [Giving access to other people](#giving-access-to-other-people)
  - [1. The policy, before the invite](#1-the-policy-before-the-invite)
  - [2. The invite](#2-the-invite)
  - [3. Check from the person's device](#3-check-from-the-persons-device)
- [The devices of the people who connect](#the-devices-of-the-people-who-connect)
  - [On all of them, install and set up Tailscale](#on-all-of-them-install-and-set-up-tailscale)
  - [Android](#android)
  - [iPhone](#iphone)
  - [Desktop](#desktop)
- [Live view and port 8555](#live-view-and-port-8555)
- [At home without Tailscale](#at-home-without-tailscale)
- [Common problems](#common-problems)

## Why HTTPS

On `http://`, the browser treats the page as insecure and hides some of its
features from it. In dwnvr, this shows up in three places:

- **Installing as an app.** Chrome only installs a page served over `https://`.
  On `http://`, Android creates a shortcut that opens in a regular tab.
- **Sharing the image.** On a phone, the `⤓ imagem` (image) button in the
  recordings opens the system share sheet, which sends the image straight to
  the gallery or to a chat. On `http://` that sheet doesn't exist, and the
  image is just downloaded.
- **Timeline thumbnails.** On `https://` they are decoded with WebCodecs. On
  `http://`, by a hidden `<video>`, a little slower.

**And the dwnvr password stops crossing the network in the clear.**

The **diagnóstico** (diagnostics) tab of the page shows "endereço seguro
(https)" (secure address) and, once installed, "app instalado (PWA)"
(installed app).

## Why not expose the server to the internet

Opening a port on the router, or publishing dwnvr through a public tunnel
(such as Tailscale Funnel or Cloudflare Tunnel), leaves the login page in
plain view of anyone. The dwnvr login becomes the only protection of the
cameras, and it wasn't built for that: the
[attempt limit](configuration.md#login-attempt-limit) is per
address, and someone trying from many addresses at once isn't held back by it.

With Tailscale, the server only answers devices that joined the VPN. From the
rest of the internet, there is nothing to find.

## The server: Tailscale and `tailscale serve`

### 1. Tailscale on the server

Install Tailscale on the dwnvr machine, from the
[download page](https://tailscale.com/download), and sign in with your account.
The network of devices on your account is your **tailnet**.

In the [Tailscale admin](https://login.tailscale.com/admin/dns), leave
**MagicDNS** and **HTTPS Certificates** on. MagicDNS gives each device a name
like `servidor.tail1234.ts.net`, and HTTPS Certificates lets Tailscale issue a
certificate for that name.

**Pick the name before installing the app on any device.** The address
`https://servidor.tail1234.ts.net` is made of the machine name and the tailnet
name, and it is the identity of the installed app: changing either one later
means installing the app again on every device.

### 2. The `serve`

On the server:

```sh
# 8080 is the port dwnvr listens on on the server, the one in docker-compose.yml
sudo tailscale serve --bg 8080
```

The `serve` listens on `https://<machine>.<tailnet>.ts.net`, on port 443, with
a Let's Encrypt certificate, and forwards to dwnvr at `http://127.0.0.1:8080`.
`--bg` saves the configuration: it comes back on its own after the server
restarts. `sudo` is no longer needed after a
`sudo tailscale set --operator=$USER`.

To check and to undo:

```sh
tailscale serve status   # https://servidor.tail1234.ts.net -> http://127.0.0.1:8080
sudo tailscale serve reset
```

The first access can take a few seconds, while the certificate is issued. The
machine name is recorded in the public Certificate Transparency logs, as
happens with every Let's Encrypt certificate.

On the dwnvr side, nothing changes. The page's URLs are relative, and the live
view WebSocket switches to `wss://` on its own. The `serve` reports that the
connection arrived over HTTPS (`X-Forwarded-Proto`), and with that the session
cookie is issued `Secure`. It also reports the tailnet address of whoever
opened the page (`X-Forwarded-For`), and that is what the login uses to count
the attempts of each device (see
[Login attempt limit](configuration.md#login-attempt-limit)).
The `serve` discards the header the device sends, so nobody escapes the limit
by making up an address.

`http://<machine-ip>:8080` keeps working on the home network. For the browser
it is another origin: separate login, and none of the HTTPS features.

### 3. The cost

The `serve` TLS spends CPU on the server. Measured on an Orange Pi Zero 3:
about 2.7x the CPU per byte of a connection without TLS. In practice, the live
view grid full over MSE (see [Live view and port 8555](#live-view-and-port-8555))
would cost about 6% of one core, and a 100 MB export takes about 80 s.

## Giving access to other people

Each person uses their own Tailscale account, and you **share the dwnvr
machine** with them. **Don't invite the person to your tailnet**: as a member,
they would see all your devices, and, if the server advertises a subnet route,
traffic from their network could go through yours. With sharing they see only
the shared machine, and subnet routes don't come along.

Who reaches what is decided in two places:

- **The sharing** decides *which machine* the person sees.
- **Your tailnet's policy** decides *which ports* of that machine they reach.

### 1. The policy, before the invite

The default policy allows everything for everyone (`"src": ["*"]`), and the `*`
includes whoever received a share. Without changing the policy, the person
reaches every port on the server: dwnvr without HTTPS on 8080, ssh and
whatever else is listening.

In [Access controls](https://login.tailscale.com/admin/acls/file), in the JSON
editor:

Starting from the default policy of a new tailnet, the change is this (`-`
goes out, `+` comes in):

```diff
 {
+    // The server's IP on the tailnet: `tailscale ip -4`, run on it.
+    "hosts": {
+        "servidor": "100.x.y.z",
+    },
+
     "grants": [
-        {"src": ["*"], "dst": ["*"], "ip": ["*"]},
+        // Your devices: everything, as before.
+        {"src": ["autogroup:member"], "dst": ["*"], "ip": ["*"]},
+
+        // Whoever received the share: only the `tailscale serve`.
+        {"src": ["autogroup:shared"], "dst": ["servidor"], "ip": ["tcp:443"]},
     ],
 }
```

That is:

- the name `servidor` becomes the server's IP on the tailnet;
- the `"*"` of the allow-everything rule becomes `"autogroup:member"`, only
  your devices;
- the `autogroup:shared` rule comes in, for whoever received the share.

The rest of the policy (`ssh`, `nodeAttrs`, the comments) stays as it is.

If your policy uses `acls` instead of `grants`, the equivalent rule is
`{"action": "accept", "src": ["autogroup:shared"], "proto": "tcp", "dst": ["servidor:443"]}`.

Before saving, the **Preview rules** tab shows what your user reaches. It must
still reach everything. A device with a tag doesn't fall under
`autogroup:member`: if any of yours has a tag, it needs a rule of its own.

### 2. The invite

In [Machines](https://login.tailscale.com/admin/machines), on the server's
row: **⋯ > Share...**, by e-mail or by link. The link has to be opened with
the same account the person uses in the Tailscale app. One invite per person.

Whoever receives it reaches the server only by the full name,
`https://servidor.tail1234.ts.net`. The short name, `servidor` alone, works
only on the devices that join Tailscale with your account, not on those of
whoever received the share.

### 3. Check from the person's device

- `https://servidor.tail1234.ts.net` opens the dwnvr login;
- `http://servidor.tail1234.ts.net:8080` **does not open**. If it does, the
  policy didn't take.

Each person signs in with their own user: create it in the **Usuários**
(Users) tab of dwnvr and send the link that appears, so they set the password.
The link is valid for 10 minutes, so send it when they already have Tailscale
on and can open it right away.

## The devices of the people who connect

The phone or computer of each person who will use dwnvr, yours included: first
Tailscale, then dwnvr through the browser.

The dwnvr session lasts 30 days and renews with use: whoever opens the app
now and then doesn't need to sign in again.

### On all of them, install and set up Tailscale

1. The Tailscale app, with the person's account. For someone who never used
   it, signing in creates their tailnet.
2. Accept the share, through the invite.
3. **Turn off the device's key expiry.** By default, each device has to sign
   in to Tailscale again every 180 days, and until then the dwnvr app simply
   stops opening. The device's owner turns it off, in the admin of their own
   tailnet: [Machines](https://login.tailscale.com/admin/machines)
   > the device > **⋯ > Disable key expiry**.

### Android

- **Open and sign in:** in Chrome, `https://servidor.tail1234.ts.net`, with
  your own user and password in dwnvr.
- **Install:** on the same page, ⋮ menu > **Instalar app** (Install app).
- **Tailscale always on:** Settings > VPN > Tailscale's gear > **VPN sempre
  ativa** (Always-on VPN). Leave "block connections without VPN" off: with it
  on, a Tailscale failure leaves the phone with no internet at all.
- **Private DNS** on **Automatic** (or off). With a fixed provider, such as
  `dns.google`, the `.ts.net` name usually stops resolving.

Android has no rule to turn Tailscale on only when away from home. It stays on
all the time, at home and away, and that is how the app opens in both places.

### iPhone

> This section hasn't been tested on an iPhone yet.

- **Open and sign in:** in Safari, `https://servidor.tail1234.ts.net`, with
  your own user and password in dwnvr.
- **Install:** on the same page, share > **Adicionar à Tela de Início** (Add
  to Home Screen). From iOS 26 on, "Open as app" is already on.
- **Tailscale always on:** in the Tailscale app, **VPN On Demand**, on for
  Wi-Fi and mobile data.
- **Recordings:** by the code, they don't play on the iPhone; live view does.
  See [`TODO_gravacoes-no-iphone.md`](TODO/TODO_gravacoes-no-iphone.md).

### Desktop

Open `https://servidor.tail1234.ts.net` in Chrome and sign in; the install
icon appears in the address bar. On Linux, Tailscale is turned on and off with
`tailscale up` and `tailscale down`.

## Live view and port 8555

Live view tries WebRTC first, for lower latency, and falls back to MSE when
WebRTC doesn't connect. The initial exchange goes through dwnvr, and therefore
through the `serve`. WebRTC media does not: it goes straight from the browser
to go2rtc's port 8555. MSE, on the other hand, comes over dwnvr's own
WebSocket.

With the policy above, whoever received the share can't reach 8555 over
Tailscale. Even so, away from home WebRTC can connect over the internet,
crossing the home NAT, with no open port (hole punching). When the carrier's
network doesn't allow it, live view falls back to MSE and plays all the same,
with a little more delay. WebRTC is an optimization, not a requirement.

> The possible exception is the iPhone with an H265 camera, which may depend
> on WebRTC to play live view. Not tested yet.

For WebRTC to always go through Tailscale, go2rtc would need to advertise the
tailnet address (`webrtc.candidates` in `go2rtc.yaml`), and the policy would
need to allow 8555, over udp and tcp, for `autogroup:shared`. Also not tested.

## At home without Tailscale

> This path hasn't been tested.

The same installed app can open at home with Tailscale off, but two pieces are
needed on the home network:

1. A local DNS that answers `servidor.tail1234.ts.net` with the server's IP on
   the home network. Many routers don't have this feature; making the server
   itself the home's DNS works, but makes the internet of the whole house
   depend on it.
2. HTTPS on port 443 of the home network, with the same certificate (the
   `tailscale cert` issues it as a file), renewed automatically. It has to be
   443: another port is another origin, and the installed app doesn't
   recognize it.

In most homes it isn't worth it. Android keeps Tailscale always on anyway. The
one who gains is the iPhone, which can turn Tailscale off on the home Wi-Fi
through VPN On Demand. The address is the same on both paths, so deciding
later causes no rework.

With no Tailscale anywhere, not even on the server, the path is a different
one: [https-local-network.md](https-local-network.md).

## Common problems

**The app opens the browser's "no connection" page.** Tailscale is off on the
device. Turn it on, or leave it always on (Always-on VPN on Android, VPN On
Demand on the iPhone).

**It worked, and after months it stopped.** The device's key expiry ran out:
the Tailscale app asks to sign in again. Sign in, and turn off the key expiry
(see [On all of them, install and set up Tailscale](#on-all-of-them-install-and-set-up-tailscale)).

**The `.ts.net` name doesn't resolve on Android.** Private DNS is fixed to a
provider. Set it to Automatic.

**The short name doesn't work.** For whoever received the share, `servidor`
alone doesn't resolve; only the full name, `servidor.tail1234.ts.net`.

**`servidor.local` stopped resolving with Tailscale on.** On Android, `.local`
names don't resolve with the VPN on, not even on mobile data. Use the
`https://` address.

**The first access was slow.** It is the certificate being issued. The next
ones are immediate.

**The install button doesn't appear.** Check that the address is `https://`.
On `http://`, even by the home network IP, the browser doesn't install.
