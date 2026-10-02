# Sendspin Bridge

AirPlay 1 targets for Sendspin speakers, stereo pairs and groups. By default,
every unpaired discovered speaker gets its own target; pairing two speakers
replaces their individual targets with one stereo destination. AirPlay PCM stays
local: libraop decodes it, then the bridge sends it to Sendspin sessions. Spotify
Connect can feed the same destinations via librespot's PCM pipe.

## Docker / homelab

```sh
git clone https://github.com/hkfuertes/sendspin-bridge.git
cd sendspin-bridge
docker build --target runtime -t sendspin-bridge:latest .
docker compose up -d
# The image build compiles the libraop CFFI extension and runs Python tests.
```

Host networking is required for mDNS, AirPlay and Spotify Connect discovery.
The first image build also compiles librespot (Rust), which takes longer.

## Spotify Connect

A Spotify Connect target is advertised alongside each exposed unpaired speaker,
stereo pair and group. Both AirPlay and Spotify remain available. When multiple
inputs play to a speaker, the last one to start wins for that speaker; if it
stops, the previous active input resumes. Stereo routing, group membership
and per-speaker sync offsets apply to both protocols. Spotify names match the
AirPlay names; librespot derives the Connect identity from the name, so duplicate
names get a numeric suffix (` 2`) and names are capped at 62 bytes for mDNS.
librespot is built with a small patch (`patches/librespot`) that adds an
`external` zeroconf backend: it only serves the pairing endpoint, and the bridge
publishes `_spotify-connect._tcp` with python-zeroconf, like the AirPlay records.
A **Spotify Premium** account is required.
Select the destination in Spotify on the trusted LAN; librespot stores reusable
credentials under `state/spotify/` (or next to `config.xml` in the add-on), in
private per-target directories. Do not expose the Spotify pairing ports or the
configuration API outside the trusted LAN.

Spotify's volume slider adjusts its own PCM. The dashboard's live
speaker/group volume adjusts the physical Sendspin players; these two controls
are independent (the dashboard cannot push volume back to Spotify). Renaming a
Spotify target changes its Connect identity; reconnect it from Spotify if needed.
Without the `librespot` binary the AirPlay bridge still works, but Spotify targets
are unavailable. `-spotify-bin ''` disables them explicitly when running locally.

## Visual configuration

Sendspin Bridge serves its single-page editor on port `8080`; open
`http://192.168.1.10:8080/` to edit speaker names, visibility, signed group
offsets, stereo pairs, groups, and the live volume of connected speakers. Volume is not
persisted. Discovery-managed IDs, ports, connection direction, and endpoints
stay out of the UI. Its fixed token is compiled into the internal UI,
so do not expose this listener outside a trusted LAN.

Saving validates and atomically writes `config.xml`, then restarts the bridge
so the new configuration takes effect. Active playback stops during that brief
restart. Use `-web-host` and `-web-port` to change the listener.

## Home Assistant add-on

Add this repository under Settings → Add-ons → Add-on store → Repositories,
then install **Sendspin Bridge**. Bump `sendspin-bridge/config.yaml`'s
`version` and merge it to `main` to publish one image tag with amd64, arm64 and
arm/v7 variants; Docker picks the right variant automatically. Home Assistant
supports amd64 and aarch64; arm/v7 is available for direct Docker use.
As speakers are discovered, the bridge writes
`config.xml` in the add-on's config folder. The repository and its GHCR packages
must be public (or the registry added to Supervisor) for Home Assistant to fetch
them. The add-on slug is `sendspin_bridge`; fresh `config.xml` files use the
`<sendspin-bridge>` root.

## `state/config.xml`

The bridge creates and atomically updates this file as speakers appear. It is
runtime state and intentionally ignored by Git. Prefer the visual editor; a
manual edit requires a bridge restart.

`dependencies.lock` pins libraop. Docker clones and patches it, then links its
PCM receiver into the Python CFFI extension. Sendspin uses the official
[`aiosendspin`](https://github.com/Sendspin/aiosendspin) package; no vendor
source is checked in.

```xml
<sendspin-bridge version="1" exposed_suffix=" (Sendspin)">
  <speakers>
    <speaker id="cocina" client_id="echo-kitchen"
             exposed_name="Cocina" direction="outbound" port="7000"
             exposed="true" delay_ms="0">
      <endpoint instance="kitchen._sendspin._tcp.local."
                host="192.168.1.50" port="8928" path="/sendspin"/>
    </speaker>
    <speaker id="salon" exposed_name="Salón" direction="inbound" port="7010"
             exposed="false"><endpoint path="/sendspin"/></speaker>
  </speakers>
  <stereos>
    <stereo id="pareja" exposed_name="Salón estéreo" port="7020"
            left_id="cocina" right_id="salon"/>
  </stereos>
  <groups>
    <group id="casa" exposed_name="Toda la casa" port="7030">
      <speaker id="cocina"/>
      <speaker id="salon"/>
    </group>
  </groups>
</sendspin-bridge>
```

- `id` is a stable, human-readable config key; `client_id` is the Sendspin
  identity, never the friendly name. `exposed_name` is the published name;
  `exposed_suffix` is appended to every speaker, stereo and group name. Set it to `""`
  to omit it.
- `direction="outbound"`: bridge discovers and dials `_sendspin._tcp`.
- `direction="inbound"`: a player discovers the bridge's
  `_sendspin-server._tcp` service and connects to it on `-server-port`.
- Exactly one direction is allowed per speaker.
- `exposed="false"` stops advertising an unpaired speaker's own target.
  Pairing also suspends both individual AirPlay targets without changing their
  stored `exposed` preferences; removing the pair restores each preference.
  Speakers still play in their pairs and groups. Outside either, the bridge
  leaves unexposed players alone (no Sendspin session) for another server.
- `delay_ms` offsets only that speaker's group or stereo audio (−500–500 ms): positive
  holds it back and negative advances it. It works for both `inbound` and
  `outbound` speakers. The first hello writes `delay_ms="0"`; after that the
  XML value is authoritative.
- Each `<stereo>` is an AirPlay target with two distinct speakers: `left_id`
  receives the left channel and `right_id` the right, each duplicated to both
  output channels so mono-only or stereo devices work. A speaker can belong to
  at most one pair. If one side disconnects, the other plays the full mix.
  Physical speakers stay in `<speakers>` for discovery, status, live volume and
  synchronization settings; the dashboard nests them under Stereo while paired.
  Existing configurations without `<stereos>` continue to load unchanged.
- Each `<group>` is advertised as its own AirPlay target
  (`port` is filled in if missing). It plays in sync on every member `speaker
  id`. A stereo pair is added to the group by listing both of its speaker IDs;
  listing only one is rejected. The dashboard offers the pair as one membership
  choice. The bridge mixes local S16 PCM on a shared 20 ms grid and gives every
  member the same Sendspin timestamp, routing left/right only for paired members.
  Unpaired exposed members stay advertised on their own; paired members are not
  advertised individually but remain connected. Speakers can receive individual,
  stereo and group targets; if several play, the most recently started input wins
  on each speaker. Group volume moves the members' average and keeps their
  differences; at 0 or 100 every member ends up equal. During playback,
  dashboard volume changes are reported to the AirPlay sender via DACP when
  available: one level for the active individual target or group, never its
  members separately. AirPlay senders without DACP cannot receive updates.

## AI-assisted development

This project was developed with generative-AI assistance. AI was used to inspect
and discuss the codebase, draft and edit code, tests, and documentation, and
help investigate runtime and CI issues. Human maintainers review changes before
merging and remain responsible for releases, security, and support.

## Acknowledgements

- [libraop](https://github.com/philippe44/libraop) (AirCast/RAOP), by
  Philippe44, provides the AirPlay receiver and PCM decoding foundation.
- [aiosendspin](https://github.com/Sendspin/aiosendspin) provides the official
  Python Sendspin protocol implementation, sessions, audio conversion, and
  discovery.
- [python-zeroconf](https://github.com/python-zeroconf/python-zeroconf)
  provides local multicast DNS advertisement for AirPlay and Spotify Connect.
- [librespot](https://github.com/librespot-org/librespot) provides the Spotify
  Connect receiver and PCM output.
- [Home Assistant](https://www.home-assistant.io/) provides the add-on platform.
