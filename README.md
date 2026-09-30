# AirPlay Sendspin

AirPlay 1 targets for Sendspin speakers and groups. By default, every
discovered speaker gets its own target; set `hidden="true"` to reserve it for
groups only. AirPlay PCM stays local: libraop decodes it, then the bridge sends
it directly to that speaker's Sendspin session.

## Docker / homelab

```sh
git clone https://github.com/hkfuertes/airplay-sendspin.git
cd airplay-sendspin
docker build --target runtime -t airplay-sendspin:latest .
docker compose up -d
# The image build runs vet and all Go tests.
```

Host networking is required for mDNS and AirPlay discovery.

## Home Assistant add-on

Add this repository under Settings → Add-ons → Add-on store → Repositories,
then install **AirPlay Sendspin**. Bump `airplay-sendspin/config.yaml`'s
`version` and merge it to `main` to publish matching native amd64 and aarch64
images; the workflow builds one architecture at a time. Home Assistant pulls
the image tag with that version. As speakers are discovered, the bridge writes
`config.xml` in the add-on's config folder. The repository and its GHCR packages
must be public (or the registry added to Supervisor) for Home Assistant to fetch
them. GitHub Releases are published manually and include Linux amd64 and arm64
binary tarballs.

## `state/config.xml`

The bridge creates and atomically updates this file as speakers appear. It is
runtime state and intentionally ignored by Git; restart the bridge after a
manual edit.

`dependencies.lock` pins the upstream commits. Docker clones them, applies the
ordered patches in `patches/libraop/` and `patches/sendspin/`, then rebuilds
libraop before building the bridge binary; no vendor source is checked in.

```xml
<airplay-sendspin version="1" airplay_suffix=" (Sendspin)">
  <speakers>
    <speaker id="cocina" client_id="echo-kitchen"
             airplay_name="Cocina" direction="outbound" port="7000"
             hidden="false" delay_ms="0">
      <endpoint instance="kitchen._sendspin._tcp.local."
                host="192.168.1.50" port="8928" path="/sendspin"/>
    </speaker>
  </speakers>
  <groups>
    <group id="casa" airplay_name="Toda la casa" port="7020">
      <speaker id="cocina"/>
      <speaker id="salon"/>
    </group>
  </groups>
</airplay-sendspin>
```

- `id` is a stable, human-readable config key; `client_id` is the Sendspin
  identity, never the friendly name. `airplay_suffix` is appended to every
  speaker and group name; use `airplay_suffix=""` to omit it.
- `direction="outbound"`: bridge discovers and dials `_sendspin._tcp`.
- `direction="inbound"`: a player discovers the bridge's
  `_sendspin-server._tcp` service and connects to it on `-server-port`.
- Exactly one direction is allowed per speaker.
- `hidden="true"` stops advertising the speaker's own AirPlay target. It still
  plays its groups; outside any group the bridge leaves the player alone (no
  Sendspin session), so another server can use it.
- `delay_ms` offsets only that speaker's group audio (−500–500 ms): positive
  holds it back and negative advances it. It works for both `inbound` and
  `outbound` speakers. The first hello writes `delay_ms="0"`; after that the
  XML value is authoritative.
- Each `<group>` is written by hand and is advertised as its own AirPlay target
  (`port` is filled in if missing). It plays in sync on every member `speaker
  id`, at 48 kHz/16-bit. Visible members stay advertised on their own; hidden
  members are not advertised but remain connected to feed their groups. If a
  speaker's own target and one of its groups play at once, the speaker mixes
  both. Group volume moves the members' average and keeps their differences (as
  in aiosendspin); at 0 or 100 every member ends up equal.

## AI-assisted development

This project was developed with generative-AI assistance. AI was used to inspect
and discuss the codebase, draft and edit code, tests, and documentation, and
help investigate runtime and CI issues. Human maintainers review changes before
merging and remain responsible for releases, security, and support.

## Acknowledgements

- [libraop](https://github.com/philippe44/libraop) (AirCast/RAOP), by
  Philippe44, provides the AirPlay receiver and PCM decoding foundation.
- [sendspin-go](https://github.com/Sendspin/sendspin-go) provides the Sendspin
  protocol implementation, sessions, and discovery support.
- [HashiCorp mDNS](https://github.com/hashicorp/mdns) provides local multicast
  DNS advertisement and discovery.
- [Home Assistant](https://www.home-assistant.io/) provides the add-on platform.
