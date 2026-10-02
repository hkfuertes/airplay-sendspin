# Sendspin Bridge

By default, each unpaired discovered Sendspin speaker is advertised as an AirPlay
target. Pairing two speakers replaces their individual AirPlay targets with one
stereo destination, which can also join a synchronized multiroom group.

The bridge writes discovered speakers to `config.xml` in this add-on's config
folder. For a GitHub repository, Supervisor mounts that folder at
`/addon_configs/<repository-id>_sendspin_bridge/` in File editor or Samba,
and at `/config` inside this add-on. Edit `config.xml` to rename targets, change
the shared suffix (`exposed_suffix`), or add `<stereos>` and `<groups>`, then
restart the add-on. Prefer the internal visual editor at port 8080; saving there
restarts the bridge automatically. Speaker, stereo and group volume controls
are live only and are not written to `config.xml`. Do not expose that listener
outside the trusted LAN.

Set `exposed="false"` on a speaker to remove its individual target even when
unpaired. Pairing suspends both individual targets without overwriting these
stored preferences; unpairing restores them. The physical speakers remain in
`config.xml` and under Stereo settings for volume, status and sync offset.
Outside pairs and groups, unexposed players remain free for another Sendspin server.

The add-on workflow publishes one multi-platform image for amd64, arm64 and
arm/v7; Home Assistant supports amd64 and aarch64, while Docker can also pull
arm/v7 directly. The add-on uses the host network: AirPlay ports start at 7000 and the inbound Sendspin server
listens on 8927. See the project README for the full XML format.
