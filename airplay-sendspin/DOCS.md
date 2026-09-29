# AirPlay Sendspin

Announces every Sendspin speaker on your network as its own AirPlay target,
plus any groups you define.

Speakers are found automatically and written to `config.xml` in this add-on's
config folder (`/addon_configs/<id>_airplay_sendspin/` in the File editor or
Samba add-ons). Edit it to rename targets, hide a speaker (`hidden="true"`),
change the name suffix (`airplay_suffix`) or add `<groups>`, then restart the
add-on. See the project README for the full format.

The add-on uses the host network: AirPlay ports start at 7000 and the inbound
Sendspin server listens on 8927.
