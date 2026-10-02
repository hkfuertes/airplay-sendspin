module github.com/hkfuertes/sendspin-bridge/golibrespot

go 1.25

require (
	github.com/devgianlu/go-librespot v0.10.2
	github.com/sirupsen/logrus v1.9.3
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/devgianlu/shannon v0.0.0-20230613115856-82ec90b7fa7e // indirect
	github.com/godbus/dbus/v5 v5.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jfreymuth/pulse v0.1.2-0.20241102120944-4ffb35054b53 // indirect
	github.com/miekg/dns v1.1.69 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/xlab/vorbis-go v0.0.0-20210911202351-b5b85f1ec645 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/exp v0.0.0-20251209150349-8475f28825e9 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/tools v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// third_party/go-librespot is upstream at dependencies.lock's ref plus patches/go-librespot.
replace github.com/devgianlu/go-librespot => ../third_party/go-librespot
