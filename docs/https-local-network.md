# HTTPS on the home network, without Tailscale <!-- omit in toc -->

This guide takes dwnvr from `http://<server-ip>:8080` to
**`https://<server-ip>`**, inside the home network, without Tailscale and
without an account on any service. In front of dwnvr goes
[Caddy](https://caddyserver.com), a web server that creates its own certificate
authority (CA) and signs the server certificate with it. Each device that opens
dwnvr trusts that CA, once, and from then on the browser treats the address as
secure and **offers to install the page as an app**.

It only works inside the home. To open it away from home too, the path is
[remote access](remote-access.md), through Tailscale, which gives you
`https://` in both places.

- [Why HTTPS](#why-https)
- [What changes and what it costs](#what-changes-and-what-it-costs)
- [The server](#the-server)
  - [1. A fixed IP](#1-a-fixed-ip)
  - [2. Caddy](#2-caddy)
  - [3. Start and check](#3-start-and-check)
  - [4. The CA, for the devices](#4-the-ca-for-the-devices)
- [The devices](#the-devices)
  - [Android](#android)
  - [iPhone](#iphone)
  - [Windows](#windows)
  - [macOS](#macos)
  - [Linux](#linux)
  - [Check, on each device](#check-on-each-device)
- [On the dwnvr side](#on-the-dwnvr-side)
- [Validity and renewal](#validity-and-renewal)
- [Undoing](#undoing)
- [Common problems](#common-problems)

## Why HTTPS

dwnvr only speaks HTTP. Over `http://<server-ip>:8080`, the password goes in
the clear at login, and the session cookie goes in the clear on every request.
A compromised device on the same network reads both, and with the cookie it
gets in without knowing the password.

`https://` also unlocks in the browser what [§Why HTTPS](remote-access.md#why-https)
in remote access lists: installing as an app, sharing the image and the
WebCodecs thumbnails.

## What changes and what it costs

- **Each device trusts the CA, once.** This is the step that decides whether
  it works (see [The devices](#the-devices)). On a device without the CA, the
  browser warns that the connection is not private. **Don't accept the
  warning:** a compromised device on the network would show an identical one,
  and anyone used to accepting it can't tell the difference. Accepted, the
  warning also doesn't let you install the app.
- **The CA key stays on the server**, in `config/caddy/data`. Whoever has it
  can impersonate any site to the devices that trust it: keep the folder the
  way you keep passwords, and include it in the backup. Once lost, a new CA is
  born, and each device has to install the new one.
- **The address is the server's IP**, which for that reason has to be fixed.
- **TLS spends CPU on the server.** The `tailscale serve` one was measured
  (see [§The cost](remote-access.md#3-the-cost)); Caddy's wasn't. In memory,
  Caddy took about 15 MB, measured on a PC.

## The server

### 1. A fixed IP

The certificate is valid for an IP, and the installed app keeps the address as
its identity. If the router hands the server another IP, `https://` stops
opening, and the app has to be installed again on every device.

On the router, reserve for the server the IP it has today, the same as in
`http://<server-ip>:8080`. The option is usually called *DHCP reservation*,
*fixed IP* or *Address Reservation*, and ties the IP to the server's MAC
address.

### 2. Caddy

This step needs Docker Compose 2.24.4 (January 2024) or newer; check with
`docker compose version`.

In a single terminal: the commands from here to step 4 run in the clone's
folder and use the two variables from the first block.

```sh
# The clone's folder, the one with docker-compose.yml and .env
cd ~/dwnvr

# The installation directory (see "Installing for real" in the README) and the
# server's IP on the home network. Replace them with yours.
DWNVR_DIR=/mnt/storage/dwnvr
DWNVR_IP=192.168.15.10

# Caddy's folder, with the subfolder where it keeps the CA. Created before
# starting, so it's born yours: Caddy runs as the same user as dwnvr.
mkdir -p "$DWNVR_DIR/config/caddy/data"
echo "CADDY_CONFIG_DIR=$DWNVR_DIR/config/caddy" >> .env
```

In the README's quick test, the configuration lives in the clone itself:
`DWNVR_DIR=$PWD`.

Caddy's configuration, with the IP already filled in by the terminal:

```sh
cat > "$DWNVR_DIR/config/caddy/Caddyfile" <<EOF
{
	# The devices are the ones that trust the CA: Caddy doesn't try to install
	# it in its own container, where it would be useless.
	skip_install_trust
	# Whoever opens by IP doesn't say the server's name in the connection (the
	# SNI), and without a name Caddy in the container can't find the
	# certificate: the connection fails.
	default_sni $DWNVR_IP
	servers {
		# No HTTP/3, which would use UDP 443, closed in the compose.
		protocols h1 h2
	}
}

https://$DWNVR_IP {
	# The certificate comes from Caddy's local CA.
	tls internal
	reverse_proxy dwnvr:8080
}
EOF
```

And the service, in a `docker-compose.override.yml` next to
`docker-compose.yml`. Compose reads both together, with no option at all, and
the update's `git pull` doesn't touch the override, which `.gitignore` leaves
out. If you already have an override, the `cat >` below would wipe it: merge
the two by hand.

```sh
cat > docker-compose.override.yml <<'EOF'
# Caddy in front of dwnvr, with HTTPS on the home network.
# See docs/https-local-network.md.
services:
  dwnvr:
    # 8080 opens only on the server itself: the home network comes in through
    # Caddy. The !override replaces docker-compose.yml's list, instead of
    # adding to it.
    ports: !override
      - "127.0.0.1:8080:8080"

  caddy:
    # 2.11 gets the 2.11.x fixes on each update.
    image: caddy:2.11
    restart: unless-stopped
    # The same user as dwnvr, so the CA is born yours on disk.
    user: "${DWNVR_UID:-1000}:${DWNVR_GID:-1000}"
    ports:
      - "443:443"
      - "80:80"   # only redirects to https://
    volumes:
      # The folder, not the file: mounted alone, a Caddyfile edited later
      # wouldn't reach the container.
      - ${CADDY_CONFIG_DIR:-./config/caddy}:/etc/caddy:ro,z
      # The CA and the certificates.
      - ${CADDY_CONFIG_DIR:-./config/caddy}/data:/data:z
    # Memory cap as a safety net: Caddy takes about 15 MB.
    deploy:
      resources:
        limits:
          memory: 64M
EOF
```

8080 stays open on the server itself, but closes to the network, where it
would remain a clear-text path for the password. The `!override` is what
closes it: without it, Compose would add the two port lists together, and 8080
would stay open.

### 3. Start and check

```sh
docker compose up -d
```

> With [Podman](../README.md#com-podman-) instead of Docker:
> `PODMAN_USERNS=keep-id podman-compose --in-pod false up -d`. Not tested.
> Rootless Podman only publishes 80 and 443 with
> `net.ipv4.ip_unprivileged_port_start=80` in the server's `sysctl`.

dwnvr restarts, because its port changed, and the cameras stop recording for a
few seconds.

On the server, in the same terminal as step 2:

```sh
# "certificate obtained successfully", with the server's IP
docker compose logs caddy | grep obtained

# The whole path: the CA, the certificate and Caddy forwarding to dwnvr
curl -sS --cacert "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" \
  "https://$DWNVR_IP/api/session"
# Answers dwnvr's JSON, like {"authRequired":true,"authenticated":false}
```

In Caddy's log, the `HTTP/2 skipped` and `HTTP/3 skipped` warnings for port 80
are normal.

What matters in Caddy's folder, which `tree "$DWNVR_DIR/config/caddy"` shows
in full (with `-pug`, also the permissions and the owner):

```text
config/caddy/
├── Caddyfile
└── data/caddy/
    ├── certificates/local/192.168.15.10/   the server certificate, renewed on its own
    └── pki/authorities/local/
        ├── root.crt                        the CA, which goes to the devices
        └── root.key                        the CA key, which never leaves the server
```

From another device on the network:

- `http://<server-ip>:8080` **does not open**;
- `http://<server-ip>` leads to `https://<server-ip>`, with the not-private
  connection warning, which is expected until the device trusts the CA.

### 4. The CA, for the devices

```sh
cp "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" ~/dwnvr-ca.crt
```

This is the file each device installs. It is public: the secret is the key,
the `root.key` next to it, which never leaves the server. On the devices, the
CA shows up as **Caddy Local Authority**, followed by the year it was born.

On a server without a screen, first bring the file to your computer, over ssh,
with this command run on the computer:

```sh
scp <user>@<server-ip>:dwnvr-ca.crt .
```

Take the file to each device by a path that doesn't cross the home network in
the open: e-mail or a messenger to yourself, or a USB cable. Downloaded over
`http://` on the home network, a compromised device could swap it for another.

## The devices

The phone or computer of each person who will use dwnvr, yours included. After
installing the CA, close and reopen the browser.

### Android

1. Save `dwnvr-ca.crt` on the phone, in Downloads.
2. Settings > Security and privacy > More security settings > Encryption and
   credentials > Install a certificate > **CA certificate**, and pick the
   file. The path changes from one manufacturer to another; the Settings
   search finds it with "certificate". Android requires a screen lock (PIN,
   pattern or password) before installing.

Chrome trusts a CA installed this way, and so does Firefox, from version 120
on.

### iPhone

> This section hasn't been tested on an iPhone yet.

1. Send `dwnvr-ca.crt` by e-mail and tap the attachment in the Mail app, or
   send it by AirDrop. The iPhone says a profile was downloaded.
2. In Settings, the downloaded profile appears right at the top: tap it and
   **Install**.
3. Settings > General > About > Certificate Trust Settings: turn on the Caddy
   Local Authority under **Enable Full Trust for Root Certificates**. Without
   this step the profile stays installed, but Safari keeps warning.

### Windows

> This section hasn't been tested.

In PowerShell, in the file's folder:

```powershell
Import-Certificate -FilePath .\dwnvr-ca.crt -CertStoreLocation Cert:\CurrentUser\Root
```

Windows asks for confirmation. It applies to Chrome, Edge and Firefox from
version 120 on, for your user.

### macOS

> This section hasn't been tested.

```sh
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain dwnvr-ca.crt
```

It applies to Safari, Chrome and Firefox from version 120 on.

### Linux

> This section hasn't been tested.

On Linux, Chrome and Firefox each have their own list, and the CA goes into
both separately.

**Chrome:** through `certutil`, which comes in the `nss-tools` package on
Fedora and in `libnss3-tools` on Debian and Ubuntu. Open Chrome once first, so
the list exists.

```sh
# Chrome uses ~/.pki/nssdb when it exists; on new installs, it uses
# ~/.local/share/pki/nssdb
NSSDB=$HOME/.pki/nssdb; [ -d "$NSSDB" ] || NSSDB=$HOME/.local/share/pki/nssdb
certutil -d sql:"$NSSDB" -A -t "C,," -n dwnvr -i ~/dwnvr-ca.crt
```

**Firefox:** Settings > Privacy & Security > Certificates > **Manage
Certificates** > **Authorities** tab > **Import**. Pick the file and check
**Trust this CA to identify websites**.

### Check, on each device

- `https://<server-ip>` opens the dwnvr login with no warning at all, and the
  icon to the left of the address says the connection is secure.
- **Installing the app:** on Android, Chrome, ⋮ menu > **Instalar app**
  (Install app); on the iPhone, Safari, share > **Adicionar à Tela de Início**
  (Add to Home Screen); on the computer, the install icon in Chrome's address
  bar.
- Opened through the app, the **diagnóstico** (diagnostics) tab of the page
  shows "app instalado (PWA)" (installed app).

## On the dwnvr side

Nothing changes. The page's URLs are relative, and the live view WebSocket
switches to `wss://` on its own. Caddy reports that the connection arrived
over HTTPS (`X-Forwarded-Proto`), and with that the session cookie is issued
`Secure`.

Caddy also tells dwnvr the address of whoever opened the page
(`X-Forwarded-For`), and that is what the login uses to count the attempts of
each device (see
[Login attempt limit](configuration.md#login-attempt-limit)).
dwnvr accepts that header because Caddy arrives over the compose network,
which is in the default `trustedProxies`. Caddy discards the header the device
sends (checked with 2.11.7), so nobody escapes the limit by making up an
address.

WebRTC media keeps going straight from the browser to go2rtc's port 8555,
outside Caddy, as before (see
[§Live view and port 8555](remote-access.md#live-view-and-port-8555)).

## Validity and renewal

- **The server certificate** is valid for 12 hours, and Caddy renews it on its
  own before it expires. The same goes for the intermediate, which sits
  between it and the CA and lasts 7 days. The devices don't notice these
  swaps, because they trust the CA, not the two.
- **The CA** is valid for about 10 years, and Caddy doesn't renew it on its
  own yet. In the last two years, its log warns (`root certificate expiring
  soon`). The swap is by hand, before it expires: delete the
  `config/caddy/data/caddy` folder, run `docker compose restart caddy` and
  install the new CA on each device, deleting the old one.

The CA's expiry date:

```sh
openssl x509 -in "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" -noout -enddate
```

## Undoing

```sh
cd ~/dwnvr   # the clone's folder
rm docker-compose.override.yml
docker compose up -d --remove-orphans
```

Caddy goes away, and dwnvr opens on 8080 to the network again. The
`config/caddy` folder stays, with the CA: starting again reuses the same one,
and the devices that still have it don't reinstall anything. To undo on the
devices too, delete the CA (on Android, in Trusted credentials > User) and
uninstall the app.

## Common problems

**The browser warns that the connection is not private.** The device doesn't
trust the CA: it needs installing, or, on the iPhone, turning on full trust.
On Linux, Chrome and Firefox each have their own list. If the warning mentions
a name that doesn't match, the address typed isn't the Caddyfile's IP.

**The install button doesn't appear.** Chrome only offers to install when it
trusts the certificate, so it is the case above. And the address has to be
`https://`: over `http://`, it doesn't install.

**The connection fails immediately, with no certificate warning.** The
`default_sni` IP isn't the server's. Check the IP on both lines of the
Caddyfile and run `docker compose restart caddy`.

**The server changed IP.** Change the IP on both lines of the Caddyfile and
run `docker compose restart caddy`. The CA stays the same, and the devices
don't reinstall anything; the app does, because the address is its identity.
To avoid repeating this, reserve the IP on the router (see
[1. A fixed IP](#1-a-fixed-ip)).

**All the devices started warning again at once.** A new CA was born: the
`config/caddy/data` folder was lost or replaced. Install the new one on each
device and delete the old one.

**`http://<server-ip>:8080` still opens from another device.** The override
didn't take. Check that it is next to `docker-compose.yml` and that
`docker compose version` is 2.24.4 or newer, and run `docker compose up -d`
again.

**Caddy doesn't start, with `permission denied` on `/data`.** The
`config/caddy/data` folder didn't exist, and Docker created it as root. Run
`sudo chown -R "$(id -u):$(id -g)" "$DWNVR_DIR/config/caddy"` and start again.

**`address already in use` on 80 or 443.** Another process already uses the
port on the server. Free the port, or, if that process listens only on
another IP, bind Caddy to the home network IP in the override, as in
`"<server-ip>:443:443"` (not tested). Then start with
`docker compose up -d --force-recreate caddy`: `up -d` alone reuses the
container from the failed attempt, which starts without the ports.
