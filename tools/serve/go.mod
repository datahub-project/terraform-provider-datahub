module tools/serve

go 1.26.5

require github.com/hashicorp/terraform-plugin-docs v0.25.0

require (
	dario.cat/mergo v1.0.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/Kunde21/markdownfmt/v3 v3.1.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.5.0 // indirect
	github.com/Masterminds/sprig/v3 v3.3.0 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/adrg/frontmatter v0.2.0 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/apparentlymart/go-textseg/v17 v17.0.1 // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/bgentry/speakeasy v0.2.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/cloudflare/circl v1.6.5 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/cli v1.1.7 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-checkpoint v0.5.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/hashicorp/hc-install v0.9.5 // indirect
	github.com/hashicorp/terraform-exec v0.25.2 // indirect
	github.com/hashicorp/terraform-json v0.28.0 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/posener/complete v1.2.3 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	github.com/yuin/goldmark v1.8.5 // indirect
	github.com/yuin/goldmark-meta v1.1.0 // indirect
	github.com/zclconf/go-cty v1.19.0 // indirect
	go.abhg.dev/goldmark/frontmatter v0.3.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20230626212559-97b1e661b5df // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Use the serve-docs fork until the serve command is merged upstream.
// The fork keeps the upstream module path so all import paths are unchanged.
// To upgrade: cut a new javabrett tag from a rebase onto the target upstream
// release, update the version here, and run go mod tidy in this directory.
// tools/go.mod (which drives make generate) is independent and upgrades freely.
replace github.com/hashicorp/terraform-plugin-docs => github.com/javabrett/terraform-plugin-docs v0.25.0-javabrett.1
