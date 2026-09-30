# AirPlay Sendspin

By default, each discovered Sendspin speaker is advertised as an AirPlay target.
You can also define AirPlay groups that play through several speakers in sync.

The bridge writes discovered speakers to `config.xml` in this add-on's config
folder. For a GitHub repository, Supervisor mounts that folder at
`/addon_configs/<repository-id>_airplay_sendspin/` in File editor or Samba,
and at `/config` inside this add-on. Edit `config.xml` to rename targets, change
the shared suffix (`airplay_suffix`), or add `<groups>`, then restart the
add-on. Prefer the internal visual editor at port 8080; saving there restarts
the bridge automatically. Do not expose that listener outside the trusted LAN.

Set `hidden="true"` on a speaker to remove its individual AirPlay target. A
hidden speaker still feeds any group that contains it; outside all groups the
bridge leaves it free for another Sendspin server.

The add-on workflow builds native amd64 and aarch64 images. The add-on uses the
host network: AirPlay ports start at 7000 and the inbound Sendspin server
listens on 8927. See the project README for the full XML format.
