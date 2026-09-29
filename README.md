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
<airplay-sendspin version="1">
  <speakers>
    <speaker id="cocina" client_id="echo-kitchen"
             airplay_name="Cocina" direction="outbound" port="7000">
      <endpoint instance="kitchen._sendspin._tcp.local."
                host="192.168.1.50" port="8928" path="/sendspin"/>
    </speaker>
  </speakers>
</airplay-sendspin>
```

- `id` is a stable, human-readable config key; `client_id` is the Sendspin
  identity, never the friendly name. AirPlay advertises `airplay_name` as
  `Name (Sendspin)` to distinguish these targets.
- `direction="outbound"`: bridge discovers and dials `_sendspin._tcp`.
- `direction="inbound"`: a player discovers the bridge's
  `_sendspin-server._tcp` service and connects to it on `-server-port`.
- Exactly one direction is allowed per speaker. `<groups>` is preserved but
  inactive for now; later groups will reference `speaker id` without changing
  discovery or AirPlay identity.
