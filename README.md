# AirPlay Sendspin

One independent AirPlay 1 target per Sendspin speaker. AirPlay PCM stays local:
libraop decodes it, then the bridge sends it directly to that speaker's
Sendspin session.

```sh
git clone https://github.com/hkfuertes/airplay-sendspin.git
cd airplay-sendspin
docker compose up -d --build
# The image build runs vet and all Go tests.
```

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
             airplay_name="Cocina" direction="outbound" port="7000" delay_ms="0">
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
- `delay_ms` holds back only that speaker's group audio (0–500 ms). The first
  hello writes `delay_ms="0"`; after that the XML value is authoritative.
- Each `<group>` is written by hand and is advertised as its own AirPlay target
  (`port` is filled in if missing). It plays in sync on every member `speaker
  id`, at 48 kHz/16-bit. Members stay advertised on their own; if a speaker's
  own target and one of its groups play at once, the speaker mixes both. Group
  volume moves the members' average and keeps their differences (as in
  aiosendspin); at 0 or 100 every member ends up equal.
